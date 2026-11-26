import assert from "node:assert/strict"
import { randomBytes } from "node:crypto"
import { spawn, execFile } from "node:child_process"
import { once } from "node:events"
import { createInterface } from "node:readline"
import { test } from "node:test"
import { promisify } from "node:util"
import { HubConnectionBuilder, HttpTransportType, LogLevel } from "@microsoft/signalr"
import WebSocket from "ws"

const binary = process.env.PANGOLIN_REALTIME_BINARY
const key = process.env.PANGOLIN_REALTIME_DEMO_KEY
const run = promisify(execFile)
const enterprise = "11111111-1111-1111-1111-111111111111"
const orgOne = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
const orgTwo = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

function deadline(promise, label) {
  const timeout = AbortSignal.timeout(10_000)
  return Promise.race([
    promise,
    new Promise((_, reject) => timeout.addEventListener("abort", () => reject(new Error(label)), {once: true})),
  ])
}

async function start(t, profile, stream) {
  const child = spawn(binary, ["--listen", "127.0.0.1:0", "--profile", profile, "--stream", stream, "--stop-on-stdin-eof"], {
    stdio: ["pipe", "pipe", "pipe"],
  })
  let logs = ""
  child.stderr.on("data", line => { logs += line })
  const exited = once(child, "exit")
  t.after(async () => {
    if (child.exitCode === null) child.stdin.end()
    try {
      const [code] = await deadline(exited, "server did not drain")
      assert.equal(code, 0, logs)
    } finally {
      if (child.exitCode === null) child.kill()
    }
  })
  const lines = createInterface({input: child.stdout})
  return deadline(new Promise((resolve, reject) => {
    lines.on("line", line => {
      if (line.startsWith("LISTEN ")) resolve(line.slice("LISTEN ".length))
    })
    exited.then(([code]) => reject(new Error(`server exited during startup (${code})\n${logs}`)), reject)
  }), "server did not become ready")
}

async function client(t, url, actor) {
  const messages = []
  const listeners = new Map()
  let wire
  let heartbeat
  const pinged = new Promise(resolve => { heartbeat = resolve })
  class TrackedSocket extends WebSocket {
    constructor(...args) {
      super(...args)
      wire = this
      this.on("message", frame => {
        if (frame.toString().includes('{"type":6}\x1e')) heartbeat()
      })
    }
  }
  const connection = new HubConnectionBuilder()
    .withUrl(url, {
      transport: HttpTransportType.WebSockets,
      accessTokenFactory: () => `${key}:${actor}`,
      WebSocket: TrackedSocket,
      logMessageContent: false,
    })
    .withAutomaticReconnect([0, 100, 500])
    .configureLogging(LogLevel.None)
    .build()
  connection.keepAliveIntervalInMilliseconds = 500
  connection.serverTimeoutInMilliseconds = 4000
  connection.on("Notification", message => {
    messages.push(message)
    const resolve = listeners.get(message.data.marker)
    if (resolve) {
      listeners.delete(message.data.marker)
      resolve(message)
    }
  })
  await connection.start()
  t.after(() => connection.stop())
  return {
    connection, messages, pinged,
    invoke: (...args) => connection.invoke(...args),
    async wait(marker) {
      const seen = messages.find(message => message.data.marker === marker)
      if (seen) return seen
      return deadline(new Promise(resolve => listeners.set(marker, resolve)), `no notification: ${marker}`)
    },
    async reconnect() {
      const reconnected = new Promise(resolve => connection.onreconnected(resolve))
      wire.terminate()
      return deadline(reconnected, "official client did not reconnect")
    },
  }
}

const tenant = name => ({tenant: name, kind: "tenant", id: name, channel: "tenant"})
const org = (name, id) => ({tenant: name, kind: "org.organization", id, channel: "organization"})
const ent = (name, id = enterprise) => ({tenant: name, kind: "org.enterprise", id, channel: "enterprise"})

async function publish(from, target, marker, recipients) {
  const id = await from.invoke("Notify", target, {marker})
  assert.ok(id)
  for (const recipient of recipients) assert.equal((await recipient.wait(marker)).id, id)
  return id
}

function never(receiver, marker) {
  assert.equal(receiver.messages.some(message => message.data.marker === marker), false, marker)
}

async function invalidProtocol(url, negotiationToken) {
  const wire = new WebSocket(url.replace(/^http/, "ws") + "?id=" + negotiationToken, {
    headers: {Authorization: `Bearer ${key}:alpha/alice`},
  })
  try {
    await deadline(once(wire, "open"), "raw WebSocket handshake failed")
    const reply = once(wire, "message")
    wire.send('{"protocol":"messagepack","version":1}\x1e')
    const [message] = await deadline(reply, "unsupported protocol was not rejected")
    assert.match(JSON.parse(message.toString().slice(0, -1)).error, /Only SignalR JSON/)
  } finally {
    wire.terminate()
  }
}

for (const profile of ["tenant-only", "organization-enabled", "replacement-contract"]) {
  test(`${profile} official clients and two independent live nodes`, {timeout: 55_000}, async t => {
    assert.ok(binary && key, "set PANGOLIN_REALTIME_BINARY and PANGOLIN_REALTIME_DEMO_KEY")
    const stream = "RT_" + randomBytes(8).toString("hex")
    await run(binary, ["--provision", "--stream", stream], {timeout: 20_000})
    const url1 = await start(t, profile, stream)
    const url2 = await start(t, profile, stream)
    const a = await client(t, url1, "alpha/alice")
    const a2 = await client(t, url2, "alpha/alice")
    const b = await client(t, url2, "beta/alice")
    assert.equal(await a.invoke("Echo", "official"), "official")
    await assert.rejects(a.invoke("UnknownMethod"), /REALTIME_METHOD_UNKNOWN/)
    await assert.rejects(a.invoke("Echo", 12), /REALTIME_INVALID/)
    await assert.rejects(a.invoke("Subscribe", tenant("beta")), /PERMISSION_DENIED/)
    await assert.rejects(a.invoke("Notify", tenant("beta"), {marker: "forged"}), /PERMISSION_DENIED/)
    await a.invoke("Subscribe", tenant("alpha"))
    await a2.invoke("Subscribe", tenant("alpha"))
    await b.invoke("Subscribe", tenant("beta"))
    await publish(a, {...tenant("alpha"), userID: "alice"}, "alpha-user", [a, a2])
    await publish(b, tenant("beta"), "beta-barrier", [b])
    never(b, "alpha-user")

    await assert.rejects(a.invoke("Notify", {...tenant("alpha"), rollback: true}, {marker: "rollback"}), /REALTIME_HANDLER_FAILED/)
    await publish(a, tenant("alpha"), "commit-barrier", [a, a2])
    never(a, "rollback")
    never(a2, "rollback")

    await a2.invoke("Unsubscribe", tenant("alpha"))
    await publish(a, tenant("alpha"), "unsubscribed", [a])
    await a2.invoke("Subscribe", tenant("alpha"))
    await publish(a, tenant("alpha"), "resubscribed", [a, a2])
    never(a2, "unsubscribed")

    if (profile === "tenant-only") {
      await assert.rejects(a.invoke("Subscribe", org("alpha", orgOne)), /PERMISSION_DENIED/)
      const response = await fetch(url1 + "/negotiate?negotiateVersion=1", {
        method: "POST", headers: {Authorization: `Bearer ${key}:alpha/alice`},
      })
      assert.equal(response.status, 200)
      const negotiation = await response.json()
      assert.notEqual(negotiation.connectionId, negotiation.connectionToken)
      assert.deepEqual(negotiation.availableTransports, [{transport: "WebSockets", transferFormats: ["Text"]}])
      await invalidProtocol(url1, negotiation.connectionToken)
      const reused = new WebSocket(url1.replace(/^http/, "ws") + "?id=" + negotiation.connectionToken, {
        headers: {Authorization: `Bearer ${key}:alpha/alice`},
      })
      const status = new Promise(resolve => reused.once("unexpected-response", (_, response) => {
        resolve(response.statusCode)
        response.destroy()
      }))
      assert.equal(await deadline(status, "consumed ticket was not rejected"), 404)
      const wrongVersion = await fetch(url1 + "/negotiate?negotiateVersion=0", {method: "POST"})
      assert.equal(wrongVersion.status, 400)
      const forbiddenOrigin = await fetch(url1 + "/negotiate?negotiateVersion=1", {
        method: "POST", headers: {Origin: "https://untrusted.invalid"},
      })
      assert.equal(forbiddenOrigin.status, 403)
      await deadline(a.pinged, "SignalR heartbeat was not received")
      return
    }

    const governor = await client(t, url2, "alpha/governor")
    const otherTenantGovernor = await client(t, url1, "beta/governor")
    const bob = await client(t, url2, "alpha/bob")
    const secondEnterprise = ent("alpha", "22222222-2222-2222-2222-222222222222")
    await governor.invoke("Subscribe", ent("alpha"))
    await assert.rejects(governor.invoke("Subscribe", org("alpha", orgOne)), /PERMISSION_DENIED/)
    await assert.rejects(bob.invoke("Subscribe", org("alpha", orgOne)), /PERMISSION_DENIED/)
    await bob.invoke("Subscribe", org("alpha", orgTwo))
    await bob.invoke("Subscribe", secondEnterprise)
    await otherTenantGovernor.invoke("Subscribe", ent("beta"))
    await a.invoke("Subscribe", org("alpha", orgOne))
    await a.invoke("Subscribe", org("alpha", orgTwo))
    await a2.invoke("Subscribe", org("alpha", orgOne))
    await b.invoke("Subscribe", org("beta", orgOne))
    await publish(a, org("alpha", orgOne), "alpha-org-one", [a, a2])
    await publish(a, org("alpha", orgTwo), "alpha-org-two", [a, bob])
    await publish(governor, ent("alpha"), "alpha-enterprise", [governor])
    await publish(bob, secondEnterprise, "alpha-second-enterprise", [bob])
    await publish(otherTenantGovernor, ent("beta"), "beta-enterprise", [otherTenantGovernor])
    await publish(b, org("beta", orgOne), "beta-org-one", [b])
    never(bob, "alpha-org-one")
    never(governor, "alpha-org-one")
    never(b, "alpha-org-one")
    never(a, "alpha-enterprise")
    never(bob, "alpha-enterprise")
    never(otherTenantGovernor, "alpha-enterprise")
    never(governor, "alpha-second-enterprise")
    never(a2, "alpha-org-two")

    await a2.invoke("RevokeMyMembership", org("alpha", orgOne))
    await publish(a, org("alpha", orgOne), "revoked", [a])
    await publish(a, tenant("alpha"), "revocation-barrier", [a, a2])
    never(a2, "revoked")
    await a2.reconnect()
    await assert.rejects(a2.invoke("Subscribe", org("alpha", orgOne)), /PERMISSION_DENIED/)
    await publish(a, org("alpha", orgOne), "after-reconnect", [a])
    await a2.invoke("Subscribe", tenant("alpha"))
    await publish(a, tenant("alpha"), "reconnect-barrier", [a, a2])
    never(a2, "after-reconnect")
  })
}
