#!/usr/bin/env bash
# Local Nezha Linux amd64 installer; intentionally no upstream installer.
set -Eeuo pipefail
umask 077
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
die(){ printf 'Nezha: %s\n' "$*" >&2; exit 1; }
[[ $EUID == 0 ]] || die "Run this command in a root shell."
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || die "Only Linux amd64 is supported."
for tool in curl sha256sum python3 systemctl install flock runuser useradd getent stat find id; do
    command -v "$tool" >/dev/null || die "Missing prerequisite: $tool"
done
[[ -d /run/systemd/system ]] || die "systemd is required."
[[ $# == 1 ]] || die "Expected Dashboard TLS endpoint."
endpoint=$1
IFS= read -r secret || die "Missing Agent secret."
[[ -n "$secret" ]] || die "Missing Agent secret."
# Validate without exposing the secret in process arguments.
printf '%s\n%s\n' "$endpoint" "$secret" | python3 -c '
import sys,re
host,secret=sys.stdin.read().splitlines()
if not re.fullmatch(r"(?:[A-Za-z0-9.-]+|\[[0-9A-Fa-f:]+\]):[1-9][0-9]{0,4}",host) or int(host.rsplit(":",1)[1])>65535:
    sys.exit("Invalid TLS endpoint")
if not 1<=len(secret)<=1024 or any(not 33<=ord(c)<=126 for c in secret):
    sys.exit("Invalid Agent secret")
'
exec 9>/run/nezha-agent-install.lock
flock -n 9 || die "Another Agent installation is running."
base=/opt/nezha
root=$base/agent
managed=$root/managed
unit=/etc/systemd/system/nezha-agent.service
service=nezha-agent.service
account=nezha-agent
version=v0.30.02
repo=https://github.com/onecat/nezha-agent/releases/download
tmp=$(mktemp -d)
backup=
new_install=0
new_root_created=0
old_active=0
old_enabled=0
changed=0
started=0
rollback(){
    rc=$?
    trap - EXIT
    if (( rc != 0 && changed == 1 )); then
        systemctl stop "$service" >/dev/null 2>&1 || true
        if (( new_install == 1 )); then
            # Never discard new state. If any upgrade was reserved after
            # enrollment, keep it in place and require manual reconciliation.
            if (( started == 1 )) && ! runuser -u "$account" -- python3 -c '
import json,sys
try:
    s=json.load(open(sys.argv[1]))
    sys.exit(0 if s["task_snapshot"] is None else 1)
except Exception:
    sys.exit(1)
' "$managed/state/upgrade-state.json"; then
                printf 'Managed state retained; service stopped. Review backup: %s\n' "$backup" >&2
            else
                if (( new_root_created )) && [[ -d "$root" ]]; then mv -T "$root" "$backup/failed-new-agent" || true; fi
                if [[ -e "$backup/old-agent" ]]; then mv -T "$backup/old-agent" "$root" || true; fi
                rm -f "$unit"
                if [[ -e "$backup/old-unit" || -L "$backup/old-unit" ]]; then cp -a "$backup/old-unit" "$unit"; fi
                systemctl daemon-reload || true
                if (( old_enabled )); then systemctl enable "$service" >/dev/null 2>&1 || true; else systemctl disable "$service" >/dev/null 2>&1 || true; fi
                if (( old_active )); then systemctl start "$service" || true; fi
                printf 'Installation failed; previous installation restored where present. Backup: %s\n' "$backup" >&2
            fi
        else
            # Existing managed ledger is never reset or rolled back manually.
            if (( old_active && ! started )); then systemctl start "$service" || true; fi
            printf 'Managed reinstall failed; existing config and ledger retained. Check journalctl -u %s.\n' "$service" >&2
        fi
    fi
    rm -rf -- "$tmp"
    exit "$rc"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Refuse symlinked or writable ancestors rather than writing through them.
for p in /opt "$base" "$root"; do
    if [[ -e "$p" || -L "$p" ]]; then
        [[ -d "$p" && ! -L "$p" && $(stat -c %u "$p") == 0 ]] || die "Untrusted installation directory: $p"
        [[ -z $(find "$p" -maxdepth 0 -perm /022 -print) ]] || die "Writable installation directory: $p"
    fi
done
if systemctl is-active --quiet nezha-supervisor.service || systemctl is-enabled --quiet nezha-supervisor.service; then
    die "An earlier standalone Supervisor service exists; migrate its ledger explicitly, do not run two Agents."
fi
if [[ -e /var/lib/nezha-agent/state/upgrade-state.json || -e /etc/nezha/supervisor.json ]]; then
    die "An earlier standalone managed installation exists; do not overwrite its ledger."
fi
[[ -z $(systemctl show -p DropInPaths --value "$service") ]] || die "Existing custom service drop-ins require review."
if systemctl is-active --quiet "$service"; then old_active=1; fi
if systemctl is-enabled --quiet "$service"; then old_enabled=1; fi

download(){
    local url=$1 dest=$2
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 --retry 2 --max-filesize 104857600 --output "$dest" "$url"
}
fetch_binaries(){
    local v=$1 name hash
    [[ "$v" =~ ^v[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}$ ]] || die "Invalid installed release version."
    download "$repo/$v/SHA256SUMS" "$tmp/sums"
    for name in nezha-agent-linux-amd64 nezha-supervisor-linux-amd64; do
        hash=$(python3 -c '
import re,sys
rows=[x.split() for x in open(sys.argv[1]) if x.strip()]
matches=[x[0] for x in rows if len(x)==2 and x[1].lstrip("*")==sys.argv[2]]
if len(matches)!=1 or not re.fullmatch(r"[0-9a-f]{64}",matches[0]):sys.exit("Invalid release checksum")
print(matches[0])
' "$tmp/sums" "$name")
        if [[ $v == v0.30.02 ]]; then
            case "$name" in
                nezha-agent-linux-amd64) [[ $hash == 0a24728dc39ba0a804f073653c367e95e2152f17eab74b08e3e60cc12be1c4e8 ]] || die "Unexpected Agent release digest.";;
                nezha-supervisor-linux-amd64) [[ $hash == 4a090ad144ff4aee36a19c9383c25ff46ab88991da56238f71e0e490b56617da ]] || die "Unexpected Supervisor release digest.";;
            esac
        fi
        download "$repo/$v/$name" "$tmp/$name"
        printf '%s  %s\n' "$hash" "$tmp/$name" | sha256sum -c --status - || die "Release checksum mismatch."
    done
}
if [[ -e "$managed" || -L "$managed" ]]; then
    [[ -d "$managed" && ! -L "$managed" && -f "$managed/supervisor.json" && -f "$managed/state/upgrade-state.json" ]] || die "Incomplete managed installation; keep its state for review."
    id "$account" >/dev/null 2>&1 || die "Managed service account missing."
    [[ $(id -u "$account") != 0 && $(stat -c %u "$managed") == "$(id -u "$account")" ]] || die "Unexpected managed directory owner."
    # Reinstallation must not reset state or change the server identity.
    printf '%s\n%s\n' "$endpoint" "$secret" | runuser -u "$account" -- python3 -c '
import json,sys
endpoint,secret=sys.stdin.read().splitlines()
cfg=json.load(open(sys.argv[1]))
if cfg.get("server")!=endpoint or cfg.get("client_secret")!=secret:sys.exit("Existing Agent identity differs; refusing to overwrite it")
' "$managed/agent.json"
    changed=1
    systemctl stop "$service"
    # Read durable version only after the old process has stopped.
    if ! version=$(runuser -u "$account" -- python3 -c '
import json,sys
s=json.load(open(sys.argv[1]))
if s.get("schema")!=2 or s.get("highest_sequence",0)<1 or not s.get("current_version"):sys.exit("Invalid ledger")
if s.get("task_snapshot") is not None and not s.get("result_reported"):sys.exit("Unresolved upgrade; restart existing service for reconciliation")
print(s["current_version"])
' "$managed/state/upgrade-state.json"); then
        die "Ledger needs reconciliation."
    fi
    fetch_binaries "$version"
    # Work inside the service-owned tree as that service account, not root.
    cat "$tmp/nezha-agent-linux-amd64" | runuser -u "$account" -- bash -c '
set -eu; umask 077; dir=$1; t=$(mktemp "$dir/.reinstall-XXXXXX"); trap "rm -f -- \"$t\"" EXIT
cat >"$t"; chmod 700 "$t"; mv -T "$t" "$dir/nezha-agent"
' bash "$managed/install"
    install -m 0755 "$tmp/nezha-supervisor-linux-amd64" "$root/.supervisor-new"
    mv -T "$root/.supervisor-new" "$root/nezha-supervisor"
else
    fetch_binaries "$version"
    if ! id "$account" >/dev/null 2>&1; then useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin "$account"; fi
    [[ $(id -u "$account") != 0 ]] || die "Invalid service account."
    install -d -m 0755 "$base"
    backup=$(mktemp -d "$base/.agent-backup-XXXXXXXX")
    if [[ -e "$unit" || -L "$unit" ]]; then cp -a "$unit" "$backup/old-unit"; fi
    if (( old_active )); then systemctl stop "$service"; fi
    new_install=1
    changed=1
    if [[ -d "$root" ]]; then mv -T "$root" "$backup/old-agent"; fi
    install -d -m 0755 "$root"
    new_root_created=1
    install -d -m 0700 -o "$account" -g "$(id -gn "$account")" "$managed" "$managed/install" "$managed/state" "$managed/stage"
    install -m 0700 -o "$account" -g "$(id -gn "$account")" "$tmp/nezha-agent-linux-amd64" "$managed/install/nezha-agent"
    install -m 0755 "$tmp/nezha-supervisor-linux-amd64" "$root/nezha-supervisor"
    ca_file=
    if [[ -n "${NZ_AGENT_CA_FILE:-}" ]]; then
        install -m 0600 -o "$account" -g "$(id -gn "$account")" "$NZ_AGENT_CA_FILE" "$managed/ca.pem"
        ca_file=$managed/ca.pem
    fi
    printf '%s\n%s\n' "$endpoint" "$secret" | python3 -c '
import json,sys
endpoint,secret=sys.stdin.read().splitlines()
cfg={"server":endpoint,"client_secret":secret,"enable_ip_query":True}
if sys.argv[1]:cfg["ca_file"]=sys.argv[1]
print(json.dumps(cfg))
' "$ca_file" >"$tmp/agent.json"
    install -m 0600 -o "$account" -g "$(id -gn "$account")" "$tmp/agent.json" "$managed/agent.json"
    runuser -u "$account" -- "$root/nezha-supervisor" --enroll --config "$managed/supervisor.json" --agent-config "$managed/agent.json" --root "$managed"
fi
unset secret
# Maintain the legacy public executable path; actual worker remains atomically
# replaceable in its private install directory.
ln -sfnT managed/install/nezha-agent "$root/nezha-agent"
cat >"$tmp/nezha-agent.service" <<'UNIT'
[Unit]
Description=Nezha Agent (local managed)
Wants=network-online.target
After=network-online.target
[Service]
Type=exec
User=nezha-agent
UMask=0077
ExecStart=/opt/nezha/agent/nezha-supervisor --config /opt/nezha/agent/managed/supervisor.json
Restart=on-failure
RestartSec=15s
TimeoutStopSec=120s
KillMode=control-group
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/nezha/agent/managed
[Install]
WantedBy=multi-user.target
UNIT
unit_stage=$(mktemp /etc/systemd/system/.nezha-agent.service-XXXXXX)
install -m 0644 "$tmp/nezha-agent.service" "$unit_stage"
mv -T "$unit_stage" "$unit"
systemctl daemon-reload
systemctl reset-failed "$service" || true
started=1
systemctl start "$service"
pid=$(systemctl show -p MainPID --value "$service")
[[ "$pid" =~ ^[1-9][0-9]*$ ]] || die "Supervisor did not start."
# Supervisor verifies accepted Host/State reports within 60 seconds.
# Require the original supervisor to survive its startup deadline.
for ((i=0;i<66;i++)); do
    systemctl is-active --quiet "$service" && [[ $(systemctl show -p MainPID --value "$service") == "$pid" ]] || die "Agent failed startup or health check."
    sleep 1
done
systemctl enable "$service" >/dev/null
changed=0
printf 'Nezha Agent %s installed and running as %s.\n' "$version" "$service"
if [[ -n "$backup" ]]; then printf 'Previous files retained in %s\n' "$backup"; fi
