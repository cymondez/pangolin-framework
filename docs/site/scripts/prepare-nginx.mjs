import {mkdirSync, cpSync, writeFileSync} from 'node:fs';
import path from 'node:path';
const base = process.env.DOCS_BASE_URL || '/';
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) throw new Error('Use simple slash-separated path segments for DOCS_BASE_URL');
const out = path.resolve('.cache');
const target = path.join(out, 'nginx-html', base);
mkdirSync(target, {recursive: true});
cpSync('build', target, {recursive: true});
writeFileSync(path.join(out, 'nginx.conf'), `
worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 256; }
http {
  include /etc/nginx/mime.types;
  default_type application/octet-stream;
  access_log /dev/stdout;
  sendfile on;
  server_tokens off;
  client_body_temp_path /tmp/client_temp;
  proxy_temp_path /tmp/proxy_temp;
  fastcgi_temp_path /tmp/fastcgi_temp;
  uwsgi_temp_path /tmp/uwsgi_temp;
  scgi_temp_path /tmp/scgi_temp;
  gzip on;
  gzip_types text/css application/javascript application/json image/svg+xml;
  server {
    listen 8080;
    root /usr/share/nginx/html;
    index index.html;
    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy strict-origin-when-cross-origin always;
    error_page 404 ${base}404.html;
    location / { ${base === '/' ? 'try_files $uri $uri/ =404;' : 'return 404;'} }
    ${base === '/' ? '' : `location ${base} { try_files $uri $uri/ =404; }`}
    location = ${base}version-manifest.json { add_header Cache-Control "no-cache"; }
    location ${base}assets/ { add_header Cache-Control "public, max-age=31536000, immutable"; }
  }
}
`);
