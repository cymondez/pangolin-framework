"""M0 environment checks. Operates only on an already-running named Compose project.

Never starts/stops Docker Desktop, containers, networks or volumes. Fault injection
temporarily disables this project's NATS proxy and restores it in a finally block.
The small NATS protocol client below is a test fixture, not a framework adapter.
"""
import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def http_json(url, data=None):
    request = urllib.request.Request(
        url, data=None if data is None else json.dumps(data).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        body = response.read()
        return json.loads(body) if body else None


class Nats:
    def __init__(self, port):
        self.sock = socket.create_connection(("127.0.0.1", port), timeout=3)
        self.file = self.sock.makefile("rwb", buffering=0)
        if not self.file.readline().startswith(b"INFO "):
            self.close()
            raise RuntimeError("NATS INFO handshake missing")
        self.file.write(b'CONNECT {"verbose":false,"pedantic":true,"name":"pangolin-m0-smoke"}\r\n')
        self.sid = 0

    def close(self):
        self.file.close()
        self.sock.close()

    def request(self, subject, body=b""):
        self.sid += 1
        reply = "_INBOX.M0." + uuid.uuid4().hex
        self.file.write(f"SUB {reply} {self.sid}\r\n".encode())
        self.file.write(f"PUB {subject} {reply} {len(body)}\r\n".encode() + body + b"\r\n")
        try:
            while True:
                line = self.file.readline()
                if not line:
                    raise RuntimeError("NATS connection closed")
                if line == b"PING\r\n":
                    self.file.write(b"PONG\r\n")
                    continue
                if line.startswith(b"-ERR"):
                    raise RuntimeError(line.decode().strip())
                if not line.startswith(b"MSG "):
                    continue
                parts = line.decode().split()
                length = int(parts[-1])
                if not 0 <= length <= 1048576:
                    raise RuntimeError("invalid NATS frame size")
                payload = self.file.read(length)
                if len(payload) != length or self.file.read(2) != b"\r\n":
                    raise RuntimeError("incomplete NATS frame")
                # JetStream pull deliveries retain their original subject.
                # The subscription ID identifies the requested reply inbox.
                if len(parts) not in (4, 5) or int(parts[2]) != self.sid:
                    raise RuntimeError("unexpected NATS reply")
                return payload, parts[3] if len(parts) == 5 else None
        finally:
            self.file.write(f"UNSUB {self.sid}\r\n".encode())

    def api(self, subject, data):
        body, _ = self.request(subject, json.dumps(data).encode())
        result = json.loads(body)
        if "error" in result:
            raise RuntimeError(f"JetStream API rejected request: {result['error']}")
        return result


def verify_jwt(token, jwks, issuer):
    header, payload, signature = token.split(".")
    decode = lambda value: base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
    meta, claims = json.loads(decode(header)), json.loads(decode(payload))
    if meta.get("alg") != "RS256":
        raise RuntimeError("unexpected JWT algorithm")
    key = next(key for key in jwks["keys"] if key["kid"] == meta["kid"])
    modulus, exponent = int.from_bytes(decode(key["n"]), "big"), int.from_bytes(decode(key["e"]), "big")
    size = (modulus.bit_length() + 7) // 8
    signature_bytes = decode(signature)
    if len(signature_bytes) != size or int.from_bytes(signature_bytes, "big") >= modulus:
        raise RuntimeError("invalid signature size")
    encoded = pow(int.from_bytes(signature_bytes, "big"), exponent, modulus).to_bytes(size, "big")
    digest_info = bytes.fromhex("3031300d060960864801650304020105000420") + hashlib.sha256((header + "." + payload).encode()).digest()
    expected = b"\x00\x01" + b"\xff" * (size - len(digest_info) - 3) + b"\x00" + digest_info
    if not hmac.compare_digest(encoded, expected):
        raise RuntimeError("JWT signature verification failed")
    if claims.get("iss") != issuer or claims.get("aud") != "pangolin-bookstore" or claims.get("exp", 0) <= time.time():
        raise RuntimeError("invalid JWT claims")
    return claims


def smoke(args, checks):
    compose = ["docker", "compose", "-p", args.project, "-f", str(ROOT / "deploy/compose/compose.yaml")]
    # Verify ownership before exec or fault injection. No existing service is adopted.
    output = subprocess.run(compose + ["ps", "--format", "json"], check=True, text=True, capture_output=True, timeout=15).stdout
    containers = json.loads(output) if output.lstrip().startswith("[") else [json.loads(line) for line in output.splitlines() if line.strip()]
    expected_services = {"postgres", "nats", "toxiproxy", "test-idp"}
    if {c["Service"] for c in containers} != expected_services or any(c["Project"] != args.project or c["State"] != "running" for c in containers):
        raise RuntimeError("expected four running services in this isolated Compose project")
    by_service = {container["Service"]: container for container in containers}
    for service, target, published in [
        ("nats", 4222, args.nats_port),
        ("toxiproxy", 8474, args.toxi_port),
        ("toxiproxy", 4222, args.nats_proxy_port),
        ("test-idp", 8080, args.idp_port),
    ]:
        bindings = by_service[service].get("Publishers") or []
        if not any(binding.get("URL") == "127.0.0.1" and binding.get("TargetPort") == target and binding.get("PublishedPort") == published and binding.get("Protocol") == "tcp" for binding in bindings):
            raise RuntimeError(f"port {published} is not this project's loopback binding for {service}:{target}; no probes performed")
    checks.append("compose-project-ownership")

    def psql(sql, user="pangolin_bootstrap", password="local-bootstrap-only", host="127.0.0.1"):
        statements = sql if isinstance(sql, list) else [sql]
        commands = [part for statement in statements for part in ("-c", statement)]
        result = subprocess.run(compose + ["exec", "-T", "-e", "PGPASSWORD=" + password, "-e", "PGCONNECT_TIMEOUT=3", "postgres", "psql", "-h", host, "-U", user, "-d", "pangolin_m0", "-qAt", "-v", "ON_ERROR_STOP=1"] + commands, text=True, capture_output=True, timeout=15)
        if result.returncode:
            raise RuntimeError("PostgreSQL probe failed: " + result.stderr.strip())
        return result.stdout.strip()

    if not psql("SHOW server_version").startswith("18.7"):
        raise RuntimeError("wrong PostgreSQL version")
    if psql(["CREATE TEMP TABLE m0_probe(v int)", "BEGIN; INSERT INTO m0_probe VALUES(1); ROLLBACK", "SELECT count(*) FROM m0_probe"]) != "0":
        raise RuntimeError("database rollback probe failed")
    if psql("SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user", "pangolin_app", "local-app-only") != "f":
        raise RuntimeError("application role can bypass RLS")
    if psql("SELECT 1", host="toxiproxy") != "1":
        raise RuntimeError("PostgreSQL proxy route failed")
    checks.extend(["postgres-version", "postgres-rollback-probe", "application-role-attributes", "postgres-proxy-route"])

    client = Nats(args.nats_port)
    stream = "M0_" + uuid.uuid4().hex.upper()
    subject = "m0." + uuid.uuid4().hex
    created = False
    try:
        client.api("$JS.API.STREAM.CREATE." + stream, {"name": stream, "subjects": [subject], "storage": "file", "num_replicas": 1, "max_msgs": 100, "max_bytes": 1048576, "max_age": 300000000000})
        created = True
        ack = client.api(subject, {"event_id": "m0-smoke", "payload": "durable"})
        if ack.get("stream") != stream or not ack.get("seq"):
            raise RuntimeError("durable publish acknowledgement missing")
        client.api(f"$JS.API.CONSUMER.DURABLE.CREATE.{stream}.SMOKE", {"stream_name": stream, "config": {"durable_name": "SMOKE", "ack_policy": "explicit", "deliver_policy": "all", "filter_subject": subject}})
        payload, reply = client.request(f"$JS.API.CONSUMER.MSG.NEXT.{stream}.SMOKE", b'{"batch":1,"expires":2000000000}')
        if json.loads(payload).get("event_id") != "m0-smoke" or not reply:
            raise RuntimeError("durable consumer returned wrong message")
        client.request(reply, b"+ACK")
        checks.append("jetstream-file-publish-durable-consume-ack")
    finally:
        if created:
            client.api("$JS.API.STREAM.DELETE." + stream, {})
        client.close()

    issuer = f"http://127.0.0.1:{args.idp_port}"
    discovery = http_json(issuer + "/.well-known/openid-configuration")
    if discovery["issuer"] != issuer:
        raise RuntimeError("wrong fixture issuer")
    token = http_json(issuer + "/token", {"subject": "m0-user", "tenant_id": "00000000-0000-0000-0000-000000000001"})["access_token"]
    claims = verify_jwt(token, http_json(discovery["jwks_uri"]), issuer)
    if claims["sub"] != "m0-user":
        raise RuntimeError("wrong token subject")
    checks.append("test-idp-discovery-real-rsa-jwt")

    proxy_url = f"http://127.0.0.1:{args.toxi_port}/proxies/nats"
    proxy = http_json(proxy_url)
    if proxy["upstream"] != "nats:4222" or not proxy["enabled"]:
        raise RuntimeError("unexpected proxy target or pre-existing fault")
    healthy = Nats(args.nats_proxy_port)
    healthy.close()
    try:
        http_json(proxy_url, {"enabled": False})
        try:
            broken = Nats(args.nats_proxy_port)
        except (OSError, RuntimeError):
            checks.append("project-nats-proxy-fault-observed")
        else:
            broken.close()
            raise RuntimeError("fault switch did not break connection")
    finally:
        http_json(proxy_url, {"enabled": True})
    restored = Nats(args.nats_proxy_port)
    restored.close()
    checks.append("project-nats-proxy-restored")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", default="pangolin-m0")
    parser.add_argument("--nats-port", type=int, default=int(os.getenv("PANGOLIN_NATS_PORT", "14222")))
    parser.add_argument("--nats-proxy-port", type=int, default=int(os.getenv("PANGOLIN_NATS_PROXY_PORT", "14223")))
    parser.add_argument("--idp-port", type=int, default=int(os.getenv("PANGOLIN_IDP_PORT", "18080")))
    parser.add_argument("--toxi-port", type=int, default=int(os.getenv("PANGOLIN_TOXI_PORT", "18474")))
    args = parser.parse_args()
    report = {"baseline": "v1.3", "project": args.project, "checks": [], "status": "failed", "runtime_acceptance": "pending"}
    try:
        smoke(args, report["checks"])
        report["status"] = "passed"
    except Exception as error:
        report["error"] = str(error)
    report["commit"] = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip()
    report["working_tree_dirty"] = bool(subprocess.run(["git", "status", "--porcelain"], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip())
    sources = sorted(p for p in ROOT.rglob("*") if p.is_file() and p.relative_to(ROOT).parts[0] not in {".git", ".cache", "artifacts"} and "__pycache__" not in p.parts)
    digest = hashlib.sha256()
    for path in sources:
        digest.update(path.relative_to(ROOT).as_posix().encode() + b"\0" + path.read_bytes() + b"\0")
    report["source_sha256"] = digest.hexdigest()
    output = ROOT / "artifacts/m0-smoke.json"
    output.parent.mkdir(exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
