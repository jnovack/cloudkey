#!/bin/bash
# Phase 5 (script 00): writes a self-hosted Headscale + headplane
# coordination server (Docker Swarm stack, behind an existing Traefik).
#
# Run this ON YOUR VPS, not the Cloud Key -- this is the "everyday"
# Tailscale coordination server the Cloud Key (and your other devices)
# will eventually enroll into. See Phase-5-Headscale for the full
# walkthrough and why each piece is shaped the way it is; this script
# just writes the same files that doc shows by hand.
#
# Requires: Docker already running in Swarm mode, with Traefik already
# deployed as its own stack on an external, attachable overlay network.
# Safe to re-run: every file below is written idempotently from these
# variables, though re-running after a real deploy will require
# `make update` (not `make deploy`) to pick up changes -- see the doc.
#
# Required:
#   HS_DOMAIN=hs.example.com          # the coordination endpoint clients use
#   ADMIN_DOMAIN=headscale.example.com  # the headplane admin UI
#
# Optional:
#   BASE_DIR=/opt/docker/headscale     # default shown
#   TRAEFIK_NETWORK=traefik            # default shown - your Traefik's
#                                       # external Swarm overlay network
#   CERT_RESOLVER=http                 # default shown - your Traefik's
#                                       # HTTP-01 ACME certResolver name
#   MAGIC_DNS_BASE_DOMAIN=ts.example.com  # defaults to ts.<HS_DOMAIN's
#                                       # base domain> - must differ from
#                                       # HS_DOMAIN itself
#   CLIENT_CERT_TLS_OPTIONS=<name>@file  # an EXISTING Traefik tls.options
#                                       # block requiring a client cert,
#                                       # if you want headplane behind the
#                                       # same wall other admin UIs use.
#                                       # Omit to leave headplane's admin
#                                       # UI without a client-cert gate.
#
# See Phase-5-Headscale Part 7 before your first `make deploy`: if
# Traefik's default TLS store already has a wildcard cert covering
# either domain, it will silently never request a dedicated one for
# them -- not something this script can detect or fix for you.

set -euo pipefail

: "${HS_DOMAIN:?set HS_DOMAIN, e.g. hs.example.com}"
: "${ADMIN_DOMAIN:?set ADMIN_DOMAIN, e.g. headscale.example.com}"

BASE_DIR="${BASE_DIR:-/opt/docker/headscale}"
TRAEFIK_NETWORK="${TRAEFIK_NETWORK:-traefik}"
CERT_RESOLVER="${CERT_RESOLVER:-http}"
MAGIC_DNS_BASE_DOMAIN="${MAGIC_DNS_BASE_DOMAIN:-ts.${HS_DOMAIN#*.}}"
CLIENT_CERT_TLS_OPTIONS="${CLIENT_CERT_TLS_OPTIONS:-}"

if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
  echo "error: Docker Swarm isn't active on this host -- 'docker swarm init' first" >&2
  exit 1
fi

if ! docker network inspect "$TRAEFIK_NETWORK" >/dev/null 2>&1; then
  echo "error: network '$TRAEFIK_NETWORK' doesn't exist -- deploy Traefik first, or set TRAEFIK_NETWORK" >&2
  exit 1
fi

mkdir -p "$BASE_DIR"/{data,headplane-data}

cat > "$BASE_DIR/config.yaml" <<EOF
---
server_url: https://${HS_DOMAIN}
listen_addr: 0.0.0.0:8080

metrics_listen_addr: 127.0.0.1:9090
grpc_listen_addr: 127.0.0.1:50443
grpc_allow_insecure: false

trusted_proxies: []

noise:
  private_key_path: /var/lib/headscale/noise_private.key

prefixes:
  # Example tailnet range. Tailscale's default is 100.64.0.0/10; this
  # guide narrows it to a /16 within that CGNAT space so addresses are
  # easy to eyeball. Pick your own and keep it stable -- changing it only
  # affects newly registered nodes; existing nodes keep their DB-assigned
  # IP until re-registered.
  v4: 100.100.0.0/16
  v6: fd7a:115c:a1e0::/48
  allocation: sequential

# Not self-hosting DERP for a first build - see Phase-5-Headscale
# Part 2 for why the public Tailscale DERP servers are fine here.
derp:
  server:
    enabled: false
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  paths: []
  auto_update_enabled: true
  update_frequency: 24h

disable_check_updates: false

node:
  expiry: 0
  ephemeral:
    inactivity_timeout: 30m

database:
  type: sqlite
  debug: false
  gorm:
    prepare_stmt: true
    parameterized_queries: true
    skip_err_record_not_found: true
    slow_threshold: 1000
  sqlite:
    path: /var/lib/headscale/db.sqlite
    write_ahead_log: true
    wal_autocheckpoint: 1000

# TLS is handled by Traefik in front of this container.
acme_url: https://acme-v02.api.letsencrypt.org/directory
acme_email: ""
tls_letsencrypt_hostname: ""
tls_letsencrypt_cache_dir: /var/lib/headscale/cache
tls_letsencrypt_challenge_type: HTTP-01
tls_letsencrypt_listen: ":http"
tls_cert_path: ""
tls_key_path: ""

log:
  level: info
  format: text

policy:
  mode: database

dns:
  magic_dns: true
  base_domain: ${MAGIC_DNS_BASE_DOMAIN}
  override_local_dns: true
  nameservers:
    global:
      - 1.1.1.1
      - 1.0.0.1
      - 2606:4700:4700::1111
      - 2606:4700:4700::1001
    split: {}
  search_domains: []
  extra_records: []

unix_socket: /var/run/headscale/headscale.sock
unix_socket_permission: "0770"

logtail:
  enabled: false

taildrop:
  enabled: true

auto_update:
  enabled: false
EOF

cat > "$BASE_DIR/acl.hujson" <<'EOF'
{
  // Starter policy: allow all traffic between all of your own devices.
  // Tighten this once more than a couple of devices are enrolled.
  //
  // Not the live source of truth - policy.mode is "database" (required
  // for headplane's ACL editor to work at all; "file" mode makes it
  // read-only). This file is the seed used once via
  // `headscale policy set --file acl.hujson` (make policy-set). Edit
  // through headplane's ACL editor going forward, or re-run
  // `make policy-set` after editing this file by hand - either way,
  // `make policy-get` shows what's actually live.
  "acls": [
    {
      "action": "accept",
      "src": ["*"],
      "dst": ["*:*"],
    },
  ],
}
EOF

cat > "$BASE_DIR/headplane-config.yaml" <<EOF
server:
  host: "0.0.0.0"
  port: 3000
  base_url: "https://${ADMIN_DOMAIN}"
  cookie_secret_path: /run/secrets/headplane-cookie-secret
  cookie_secure: true
  cookie_max_age: 86400
  data_path: /var/lib/headplane

headscale:
  # Internal Swarm service-to-service call, never the public HS_DOMAIN.
  url: "http://headscale:8080"
  public_url: "https://${HS_DOMAIN}"
  config_path: /etc/headscale/config.yaml

integration:
  agent:
    enabled: false

  docker:
    enabled: true
    container_label: "me.tale.headplane.target=headscale"
    socket: "unix:///var/run/docker.sock"

  kubernetes:
    enabled: false
    validate_manifest: true
    pod_name: "headscale"

  proc:
    enabled: false
EOF

client_cert_line=""
if [ -n "$CLIENT_CERT_TLS_OPTIONS" ]; then
  client_cert_line="        - \"traefik.http.routers.headplane-secure.tls.options=${CLIENT_CERT_TLS_OPTIONS}\""
fi

cat > "$BASE_DIR/headscale.yml" <<EOF
version: '3.6'

services:
  headscale:
    image: headscale/headscale:v0.29.2
    command: serve
    networks:
      - ${TRAEFIK_NETWORK}
    volumes:
      - /etc/localtime:/etc/localtime:ro
      - ${BASE_DIR}/config.yaml:/etc/headscale/config.yaml:ro
      - ${BASE_DIR}/acl.hujson:/etc/headscale/acl.hujson:ro
      - ${BASE_DIR}/data:/var/lib/headscale
    logging:
      driver: syslog
      options:
        tag: docker/headscale/headscale/{{.ID}}
    labels:
      - "me.tale.headplane.target=headscale"
    deploy:
      replicas: 1
      resources:
        limits:
          cpus: '0.20'
          memory: 128M
      placement:
        constraints:
          - node.role == manager
      restart_policy:
        condition: any
      labels:
        - "traefik.enable=true"

        - "traefik.http.routers.headscale.entrypoints=http"
        - "traefik.http.routers.headscale.rule=Host(\`${HS_DOMAIN}\`)"
        - "traefik.http.routers.headscale.middlewares=https-redirect@file"

        - "traefik.http.routers.headscale-secure.entrypoints=https"
        - "traefik.http.routers.headscale-secure.rule=Host(\`${HS_DOMAIN}\`)"
        - "traefik.http.routers.headscale-secure.tls=true"
        - "traefik.http.routers.headscale-secure.tls.certresolver=${CERT_RESOLVER}"
        - "traefik.http.routers.headscale-secure.service=headscale"
        - "traefik.http.services.headscale.loadbalancer.server.port=8080"

  headplane:
    # See Phase-5-Overview: headplane's release automation doesn't
    # reliably push versioned registry tags. Before your first deploy,
    # confirm what this digest actually is (pull it, check its
    # org.opencontainers.image.version label) rather than trusting it
    # blindly, and update it here if a newer confirmed build exists.
    image: ghcr.io/tale/headplane@sha256:7bd6523a14567a43eb4ffa1e95e3d95456b8539c9d081757df3f228b9e836fb5
    networks:
      - ${TRAEFIK_NETWORK}
    volumes:
      - /etc/localtime:/etc/localtime:ro
      - ${BASE_DIR}/headplane-config.yaml:/etc/headplane/config.yaml:ro
      - ${BASE_DIR}/config.yaml:/etc/headscale/config.yaml:ro
      - ${BASE_DIR}/headplane-data:/var/lib/headplane
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - headplane-cookie-secret
    logging:
      driver: syslog
      options:
        tag: docker/headscale/headplane/{{.ID}}
    deploy:
      replicas: 1
      resources:
        # See Phase-5-Overview: 128M is not enough, it wedges the
        # Node process into a near-permanent GC stall instead of
        # crashing it outright.
        limits:
          cpus: '0.50'
          memory: 384M
      restart_policy:
        condition: any
      labels:
        - "traefik.enable=true"

        - "traefik.http.routers.headplane.entrypoints=http"
        - "traefik.http.routers.headplane.rule=Host(\`${ADMIN_DOMAIN}\`)"
        - "traefik.http.routers.headplane.middlewares=https-redirect@file"

        - "traefik.http.routers.headplane-secure.entrypoints=https"
        - "traefik.http.routers.headplane-secure.rule=Host(\`${ADMIN_DOMAIN}\`)"
        - "traefik.http.routers.headplane-secure.tls=true"
        - "traefik.http.routers.headplane-secure.tls.certresolver=${CERT_RESOLVER}"
${client_cert_line}
        - "traefik.http.routers.headplane-secure.service=headplane"
        - "traefik.http.services.headplane.loadbalancer.server.port=3000"

networks:
  ${TRAEFIK_NETWORK}:
    external: true

secrets:
  headplane-cookie-secret:
    external: true
EOF

cat > "$BASE_DIR/Makefile" <<'EOF'
STACK      := headscale
SERVICE    := $(STACK)_headscale
CONTAINER   = $(shell docker ps -q -f name=$(SERVICE) | head -n1)

USER       ?= admin
USER_ID    ?=
EXPIRATION ?= 366d

.PHONY: deploy update rm ps logs logs-headplane shell cookie-secret \
        apikey apikey-list apikey-expire \
        user-create users \
        preauthkey preauthkey-list \
        policy-get policy-set \
        nodes routes

## --- stack lifecycle ---

deploy:
	docker stack deploy -c $(STACK).yml $(STACK)

update:
	docker stack rm $(STACK) || true
	sleep 10
	docker stack deploy -c $(STACK).yml $(STACK)

rm:
	docker stack rm $(STACK)

ps:
	docker stack ps $(STACK)

logs:
	docker service logs -f $(SERVICE)

logs-headplane:
	docker service logs -f $(STACK)_headplane

shell:
	docker exec -it $(CONTAINER) sh

## --- one-time setup: headplane's session cookie secret ---
## Run once before the first `make deploy`.

cookie-secret:
	openssl rand -hex 16 | docker secret create headplane-cookie-secret -

## --- API keys (headplane logs in with one instead of OIDC) ---

apikey:
	docker exec $(CONTAINER) headscale apikeys create --expiration $(EXPIRATION)

apikey-list:
	docker exec $(CONTAINER) headscale apikeys list

# make apikey-expire PREFIX=<key-prefix-from-apikey-list>
apikey-expire:
	docker exec $(CONTAINER) headscale apikeys expire --prefix $(PREFIX)

## --- users ---

# make user-create USER=<name>
user-create:
	docker exec $(CONTAINER) headscale users create $(USER)

users:
	docker exec $(CONTAINER) headscale users list

## --- pre-auth keys (for enrolling new nodes without an interactive login) ---
## Unlike USER above, these take the numeric user ID, not the name -
## `headscale preauthkeys create --user` rejects a name outright
## (strconv.ParseUint error). Look it up with `make users` first.

# make preauthkey USER_ID=1 EXPIRATION=1h
preauthkey:
	@if [ -z "$(USER_ID)" ]; then \
		echo "error: USER_ID required (numeric - see 'make users')"; \
		exit 1; \
	fi
	docker exec $(CONTAINER) headscale preauthkeys create \
		--user $(USER_ID) --expiration $(EXPIRATION) --reusable

preauthkey-list:
	@if [ -z "$(USER_ID)" ]; then \
		echo "error: USER_ID required (numeric - see 'make users')"; \
		exit 1; \
	fi
	docker exec $(CONTAINER) headscale preauthkeys list --user $(USER_ID)

## --- ACL policy (database-backed - see acl.hujson's own comment for why) ---

policy-get:
	docker exec $(CONTAINER) headscale policy get

# Re-seed the live (database) policy from acl.hujson, e.g. after hand-
# editing that file. headplane's own ACL editor writes straight to the
# database and doesn't touch this file, so the two can drift - policy-get
# always shows what's actually enforced.
policy-set:
	docker exec $(CONTAINER) headscale policy set --file /etc/headscale/acl.hujson

## --- inspection ---

nodes:
	docker exec $(CONTAINER) headscale nodes list

routes:
	docker exec $(CONTAINER) headscale nodes list-routes
EOF

echo
echo "Wrote $BASE_DIR/{config.yaml,acl.hujson,headplane-config.yaml,headscale.yml,Makefile}"
echo
echo "Next steps:"
echo "  1. Point DNS for $HS_DOMAIN and $ADMIN_DOMAIN at this VPS."
echo "  2. cd $BASE_DIR && make cookie-secret   # one-time"
echo "  3. make deploy"
echo "  4. make policy-set   # seeds the database from acl.hujson -- do"
echo "     this before enrolling any device, not after (policy.mode is"
echo "     'database', so skipping this leaves headscale's own default"
echo "     in place until you run it)"
echo "  5. Verify (see Phase-5-Headscale Part 7-8 -- check the served"
echo "     certificate before trusting it, not just that it deployed)."
echo "  6. make apikey   # paste into headplane's login screen"
