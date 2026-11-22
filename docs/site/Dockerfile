# syntax=docker/dockerfile:1
FROM node:22.23.4-bookworm-slim@sha256:5ec23b7c622b00547753bc6ffd78085dd868a68aeb41c8e0a1418f033e13d96d AS build
WORKDIR /workspace/docs/site
COPY docs/site/package.json docs/site/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY VERSION /workspace/VERSION
COPY examples/basics/ /workspace/examples/basics/
COPY docs/site/ ./
ARG FRAMEWORK_VERSION
ARG DOCS_URL=http://localhost:18088
ARG DOCS_BASE_URL=/
ENV DOCS_URL=$DOCS_URL DOCS_BASE_URL=$DOCS_BASE_URL
RUN test "$FRAMEWORK_VERSION" = "$(cat /workspace/VERSION)" && npm test && npm run build
# Keep URL prefixes on disk so reverse proxies can forward the path unchanged.
RUN node scripts/prepare-nginx.mjs

FROM nginx:stable-alpine@sha256:caf4e1e032a20f9c3c5e28fc7f25ffd768ad8677b4975c54193075997b3a97e3 AS runtime
ARG FRAMEWORK_VERSION
LABEL org.opencontainers.image.title="panGOlin Framework Docs" \
      org.opencontainers.image.version=$FRAMEWORK_VERSION \
      org.opencontainers.image.source="https://github.com/cymondez/pangolin-framework"
RUN rm -f /usr/share/nginx/html/index.html /usr/share/nginx/html/50x.html
COPY --from=build /workspace/docs/site/.cache/nginx-html/ /usr/share/nginx/html/
COPY --from=build /workspace/docs/site/.cache/nginx.conf /etc/nginx/nginx.conf
USER nginx
EXPOSE 8080
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
