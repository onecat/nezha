"""Isolated native integration; no production secrets or deployment."""
import hashlib
from html.parser import HTMLParser
import shlex
import http.cookiejar
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

BIN = pathlib.Path(sys.argv[1]).resolve()
ARCH = sys.argv[2]
assert ARCH in ("amd64", "arm64")
BASE = "https://127.0.0.1:18443"

def write(path, content):
    path.write_text(content)
    path.chmod(0o600)

def config(path, value):
    write(path, json.dumps(value))

def wait(label, fn, seconds=180):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            if fn():
                print(label + ": passed", flush=True)
                return
        except (urllib.error.URLError, ConnectionError, TimeoutError):
            pass
        time.sleep(1)
    raise RuntimeError("timeout: " + label)

def run():
    with tempfile.TemporaryDirectory(prefix="nezha-joint-") as d:
        work = pathlib.Path(d)
        children = []
        supervisors = {}
        observed = {}
        def start(name, args, env=None):
            with (work / (name+".log")).open("ab") as log:
                p = subprocess.Popen([str(x) for x in args], cwd=work,
                    env=env, stdout=log, stderr=log, start_new_session=True)
            children.append(p)
            return p
        def stop(p):
            if p.poll() is None:
                os.killpg(p.pid, signal.SIGTERM)
                try:
                    p.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    os.killpg(p.pid, signal.SIGKILL)
                    p.wait(timeout=10)
        try:
            password = secrets.token_hex(18)
            hashed = subprocess.check_output(["htpasswd","-nbBC","5","smoke",password],text=True).strip().split(":",1)[1]
            write(work/"dashboard.yaml", "httpport: 18008\nhttpbindhost: 127.0.0.1\ngrpcport: 15555\ngrpcbindhost: 127.0.0.1\ngrpchost: 127.0.0.1\nproxygrpcport: 15443\ntls: true\nlanguage: en-US\nlocalauth:\n  username: smoke\n  passwordhash: "+json.dumps(hashed)+"\n")
            subprocess.run(["openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN=127.0.0.1","-addext","subjectAltName=IP:127.0.0.1","-keyout",str(work/"tls.key"),"-out",str(work/"tls.pem")],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            subprocess.run(["openssl","genpkey","-algorithm","Ed25519","-out",str(work/"sign.pem")],check=True)
            (work/"sign.pem").chmod(0o600)
            pub = subprocess.check_output(["openssl","pkey","-in",str(work/"sign.pem"),"-pubout","-outform","DER"])[-32:].hex()
            (work/"packages").mkdir(mode=0o700)
            for directory in ("client","proxy","fastcgi","uwsgi","scgi"):
                (work/directory).mkdir()
            write(work/"nginx.conf", """
worker_processes 1;
pid nginx.pid;
error_log nginx-error.log;
events { worker_connections 128; }
http {
  # The legacy Agent authenticates using client_secret (gRPC metadata).
  underscores_in_headers on;
  log_format joint '$server_port $status $upstream_status $upstream_trailer_grpc_status';
  access_log joint-status.log joint;
  client_body_temp_path client;
  proxy_temp_path proxy;
  fastcgi_temp_path fastcgi;
  uwsgi_temp_path uwsgi;
  scgi_temp_path scgi;
  client_max_body_size 100m;
  server {
    listen 127.0.0.1:18443 ssl;
    ssl_certificate tls.pem;
    ssl_certificate_key tls.key;
    location / {
      proxy_set_header Host $http_host;
      proxy_set_header X-Forwarded-Proto https;
      proxy_read_timeout 180s;
      proxy_send_timeout 180s;
      proxy_pass http://127.0.0.1:18008;
    }
  }
  server {
    listen 127.0.0.1:15443 ssl http2;
    ssl_certificate tls.pem;
    ssl_certificate_key tls.key;
    location / {
      grpc_read_timeout 180s;
      grpc_send_timeout 180s;
      grpc_pass grpc://127.0.0.1:15555;
    }
  }
}
""")
            subprocess.run(["nginx","-t","-p",str(work)+"/","-c",str(work/"nginx.conf")],check=True,cwd=work)
            proxy = start("proxy", ["nginx","-p",str(work)+"/","-c",str(work/"nginx.conf"),"-g","daemon off;"])
            tls = ssl.create_default_context(cafile=str(work/"tls.pem"))
            def browser():
                return urllib.request.build_opener(urllib.request.HTTPSHandler(context=tls),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            client = browser()
            def request(route, data=None, kind="application/json", origin=BASE, expected=200, session=None):
                if isinstance(data,dict):
                    data=json.dumps(data).encode()
                headers={}
                if data is not None:
                    headers={"Content-Type":kind,"Origin":origin}
                req=urllib.request.Request(BASE+route,data=data,headers=headers)
                try:
                    with (session or client).open(req,timeout=150) as resp:
                        code,body=resp.status,resp.read()
                except urllib.error.HTTPError as exc:
                    code,body=exc.code,exc.read()
                if code!=expected:
                    raise RuntimeError(f"{route}: expected {expected}, got {code}")
                return body
            def getjson(route, **kw):
                return json.loads(request(route,**kw))
            def login():
                html=request("/login").decode()
                csrf=re.search(r'name="csrf_token" value="([^"]+)"',html)
                if not csrf:
                    raise RuntimeError("login token missing")
                request("/local/login",urllib.parse.urlencode({"username":"smoke","password":password,"csrf_token":csrf[1]}).encode(),"application/x-www-form-urlencoded")
                page=request("/server").decode()
                if 'nezha-servers' not in page:
                    raise RuntimeError("login did not reach server page")
            args=[BIN/"dashboard","-c",work/"dashboard.yaml","--db",work/"dashboard.db"]
            env=dict(os.environ)
            env.pop("NZ_MANAGED_UPGRADE_CONFIG",None)
            url_mode=os.environ.get("NZ_JOINT_URL")=="1"
            if not url_mode: env["NZ_DISABLE_URL_UPGRADE"]="1"
            dashboard=start("dashboard",args,env)
            def up():
                try:
                    return b'name="csrf_token"' in request("/login") 
                except RuntimeError:
                    return False
            wait("Dashboard login",up,60)
            # Verify actual kernel listener addresses, not just parsed YAML.
            def loopback_listeners():
                listeners=subprocess.check_output(["ss","-H","-ltn"],text=True)
                ready=True
                for port in (18008,15555):
                    matches=[line.split()[3] for line in listeners.splitlines() if len(line.split())>3 and line.split()[3].endswith(":"+str(port))]
                    if any(addr!="127.0.0.1:"+str(port) for addr in matches):
                        raise RuntimeError("unexpected non-loopback listener on port "+str(port)+": "+repr(matches))
                    ready=ready and matches==["127.0.0.1:"+str(port)]
                return ready
            wait("Explicit loopback listeners",loopback_listeners,30)
            login()
            if not url_mode: request("/api/managed-upgrade/wave",{"servers":[1],"digest":"a"*64},expected=503)
            targets=[]
            for i in range(3):
                name=f"joint-agent-{i}"
                reg=getjson("/api/v1/server/register",data={"Name":name,"Tag":"joint","HideForGuest":"on"})
                secret=reg["secret"]
                listing=getjson("/api/v1/server/list")["result"]
                sid=next(row["id"] for row in listing if row["name"]==name)
                targets.append((sid,secret))
            if url_mode:
                seed_path=work/"dashboard.db.url-upgrade"/"signing.seed"
                seed=seed_path.read_bytes()
                if len(seed)!=32 or seed_path.stat().st_mode & 0o077:
                    raise RuntimeError("automatic signing seed not securely initialized")
                sid,secret=targets[0]
                root=work/"url-agent"
                root.mkdir(mode=0o700)
                for name in ("install","state","stage","config"):
                    (root/name).mkdir(mode=0o700)
                shutil.copyfile(BIN/"agent-v0.30.00",root/"install/nezha-agent")
                (root/"install/nezha-agent").chmod(0o700)
                config(root/"config/agent.json",{"server":"127.0.0.1:15443","client_secret":secret,"ca_file":str(work/"tls.pem"),"enable_ip_query":False,"report_interval_seconds":1})
                subprocess.run([str(BIN/"supervisor"),"--enroll","--config",str(root/"config/supervisor.json"),"--agent-config",str(root/"config/agent.json"),"--root",str(root)],check=True,cwd=work)
                supervisors[sid]=start("url-supervisor",[BIN/"supervisor","--config",root/"config/supervisor.json"])
                def reports(v):
                    row=getjson("/api/v1/server/details?id="+str(sid))["result"][0]
                    return (row.get("host") or {}).get("Version")==v and bool(row.get("status"))
                wait("URL enrolled Agent reports",lambda:reports("v0.30.00"),120)
                url="https://github.com/onecat/nezha-agent/releases/download/v0.30.03/nezha-agent"
                request("/api/managed-upgrade/url",{"servers":[sid],"url":url},origin="https://invalid.example",expected=403)
                task=getjson("/api/managed-upgrade/url",data={"servers":[sid],"url":url})["tasks"][0]["Task"]["ID"]
                wait("URL actual binary upgrade",lambda:getjson("/api/managed-upgrade/status/"+task)["Task"]["State"]=="succeeded" and reports("v0.30.03"),180)
                stop(dashboard)
                dashboard=start("url-reopened",args,env)
                client=browser()
                wait("URL Dashboard restart",up,60)
                if seed_path.read_bytes()!=seed:
                    raise RuntimeError("automatic signing seed changed after restart")
                login()
                if getjson("/api/managed-upgrade/status/"+task)["Task"]["State"]!="succeeded":
                    raise RuntimeError("URL receipt lost after restart")
                print("URL release, enrollment, upgrade and persisted receipt passed",flush=True)
                if os.environ.get("NZ_JOINT_INSTALLER")=="1":
                    install_sid,install_secret=targets[1]
                    class ClipboardButtons(HTMLParser):
                        def __init__(self):
                            super().__init__(convert_charrefs=True)
                            self.buttons=[]
                        def handle_starttag(self,tag,attrs):
                            if tag=="button":
                                self.buttons.append(dict(attrs))
                    parser=ClipboardButtons()
                    parser.feed(request("/server").decode())
                    matches=[b for b in parser.buttons if b.get("data-agent-install-id")==str(install_sid)]
                    if len(matches)!=1:
                        raise RuntimeError("expected exactly one enabled installer button for selected server")
                    button=matches[0]
                    command=button.get("data-clipboard-text","")
                    digest=button.get("data-agent-installer-sha256","")
                    if not re.fullmatch(r"[0-9a-f]{64}",digest):
                        raise RuntimeError("installer button digest missing or invalid")
                    tokens=shlex.split(command[:-1]) if command.endswith(")") else []
                    url="https://github.com/onecat/nezha-agent/releases/download/v0.30.03/nezha-agent"
                    if digest not in tokens or url not in tokens or "service" not in tokens or "install" not in tokens:
                        raise RuntimeError("clipboard command missing verified binary install")
                    if install_secret not in tokens or "127.0.0.1:15443" not in tokens:
                        raise RuntimeError("clipboard command has wrong selected identity or endpoint")
                    if any("agent-install.sh" in b.get("data-clipboard-text","") or b.get("data-clipboard-text","").startswith("./nezha-agent -s ") for b in parser.buttons):
                        raise RuntimeError("obsolete installer command still present")
                    request("/agent-install.sh",expected=404,session=browser())
                    print("Clipboard command and selected binary identity passed",flush=True)
                    # Real systemd migration on the disposable native CI runner.
                    opt=pathlib.Path("/opt")
                    base_dir=opt/"nezha"
                    unit_path=pathlib.Path("/etc/systemd/system/nezha-agent.service")
                    if base_dir.exists() or base_dir.is_symlink() or unit_path.exists() or unit_path.is_symlink():
                        raise RuntimeError("installer CI must use a clean host")
                    opt_stat=opt.lstat()
                    if opt.is_symlink() or not opt.is_dir() or opt_stat.st_uid!=0:
                        raise RuntimeError("CI /opt must be a real root-owned directory")
                    original_opt_mode=opt_stat.st_mode & 0o7777
                    def sudo(*argv,**kw):
                        return subprocess.run(["sudo","-n",*map(str,argv)],**kw)
                    try:
                        # Hosted runners may give their users write access to /opt.
                        # Match a trusted production ancestor in this disposable
                        # test host only. The installer must still reject writable
                        # ancestors and must never chmod them on customer machines.
                        sudo("chmod","go-w","/opt",check=True)
                        if opt.stat().st_mode & 0o022:
                            raise RuntimeError("CI /opt remains group/world writable")
                        print("CI trusted /opt fixture prepared",flush=True)
                        sudo("install","-d","-o","root","-g","root","-m","0755","/opt/nezha","/opt/nezha/agent",check=True)
                        legacy=work/"legacy-marker"
                        legacy.write_text("legacy installation")
                        sudo("install","-m","0600",legacy,"/opt/nezha/agent/legacy-marker",check=True)
                        legacy_unit=work/"legacy.service"
                        legacy_unit.write_text("[Service]\\nExecStart=/usr/bin/sleep infinity\\n[Install]\\nWantedBy=multi-user.target\\n".replace("\\n","\n"))
                        sudo("install","-m","0644",legacy_unit,"/etc/systemd/system/nezha-agent.service",check=True)
                        sudo("systemctl","daemon-reload",check=True)
                        sudo("systemctl","enable","--now","nezha-agent.service",check=True)

                        def copied_install():
                            p=sudo("env","NZ_AGENT_CA_FILE="+str(work/"tls.pem"),"sh","-s",input=command+"\n",text=True,timeout=360)
                            if p.returncode!=0:
                                sudo("systemctl","show","-p","ActiveState","-p","SubState","-p","Result","-p","ExecMainStatus","nezha-agent.service")
                                sudo("journalctl","-u","nezha-agent.service","-n","25","--no-pager")
                                raise RuntimeError("copied installation command failed")
                        copied_install()
                        def installed_report():
                            row=getjson("/api/v1/server/details?id="+str(install_sid))["result"][0]
                            return (row.get("host") or {}).get("Version")=="v0.30.03" and bool(row.get("status"))
                        wait("Installed service accepted reports",installed_report,90)
                        sudo("systemctl","is-enabled","--quiet","nezha-agent.service",check=True)
                        sudo("systemctl","is-active","--quiet","nezha-agent.service",check=True)
                        if pathlib.Path("/opt/nezha/agent/nezha-agent").is_symlink():
                            raise RuntimeError("single binary must be the actual executable")
                        mainpid=sudo("systemctl","show","-p","MainPID","--value","nezha-agent.service",check=True,stdout=subprocess.PIPE,text=True).stdout.strip()
                        if sudo("readlink","/proc/"+mainpid+"/exe",check=True,stdout=subprocess.PIPE,text=True).stdout.strip()!="/opt/nezha/agent/nezha-agent":
                            raise RuntimeError("systemd is not directly running Agent")
                        ledger_path="/opt/nezha/agent/.state/upgrade-state.json"
                        ledger=sudo("cat",ledger_path,check=True,stdout=subprocess.PIPE).stdout
                        if json.loads(ledger)["server_id"]!=install_sid:
                            raise RuntimeError("installer enrollment identity mismatch")
                        saved_config_path="/opt/nezha/agent/agent.json"
                        saved_config=sudo("cat",saved_config_path,check=True,stdout=subprocess.PIPE).stdout
                        saved_supervisor=sudo("cat","/opt/nezha/agent/.state/identity.json",check=True,stdout=subprocess.PIPE).stdout
                        marker_paths=sudo("find","/opt/nezha","-maxdepth","3","-name","legacy-marker","-type","f",check=True,stdout=subprocess.PIPE,text=True).stdout.splitlines()
                        if len(marker_paths)!=1 or "/.agent-backup-" not in marker_paths[0] or sudo("cat",marker_paths[0],check=True,stdout=subprocess.PIPE).stdout!=b"legacy installation":
                            raise RuntimeError("legacy directory backup missing or changed")
                        before_report=getjson("/api/v1/server/details?id="+str(install_sid))["result"][0]["last_active"]
                        copied_install()
                        if sudo("cat",ledger_path,check=True,stdout=subprocess.PIPE).stdout!=ledger:
                            raise RuntimeError("reinstall changed durable ledger")
                        if sudo("cat",saved_config_path,check=True,stdout=subprocess.PIPE).stdout!=saved_config or sudo("cat","/opt/nezha/agent/.state/identity.json",check=True,stdout=subprocess.PIPE).stdout!=saved_supervisor:
                            raise RuntimeError("reinstall changed Agent configuration or pinned identity")
                        def fresh_reinstall_report():
                            row=getjson("/api/v1/server/details?id="+str(install_sid))["result"][0]
                            return (row.get("host") or {}).get("Version")=="v0.30.03" and bool(row.get("status")) and row.get("last_active",0)>before_report
                        wait("Reinstalled service accepted fresh reports",fresh_reinstall_report,90)
                        print("Systemd legacy migration and state-preserving reinstall passed",flush=True)
                        # New single-binary service upgrades without a resident
                        # supervisor. A development build of this exact source
                        # labelled .02 is used only as the previous-version fixture.
                        sudo("systemctl","disable","--now","nezha-agent.service",check=True)
                        sudo("rm","-rf","--","/opt/nezha/agent",check=True)
                        sudo("rm","-f","/etc/systemd/system/nezha-agent.service",check=True)
                        sudo("systemctl","daemon-reload",check=True)
                        direct_sid,direct_secret=targets[2]
                        bootstrap=work/"direct-agent-old"
                        shutil.copyfile(BIN/"agent-v0.30.02",bootstrap)
                        bootstrap.chmod(0o700)
                        sudo(bootstrap,"service","install","--server","127.0.0.1:15443","--ca-file",work/"tls.pem",input=direct_secret+"\n",text=True,timeout=180,check=True)
                        def direct_reports(v):
                            row=getjson("/api/v1/server/details?id="+str(direct_sid))["result"][0]
                            return (row.get("host") or {}).get("Version")==v and bool(row.get("status"))
                        wait("Direct old Agent reports",lambda:direct_reports("v0.30.02"),90)
                        tid=getjson("/api/managed-upgrade/url",data={"servers":[direct_sid],"url":url})["tasks"][0]["Task"]["ID"]
                        wait("Direct actual release self-upgrade",lambda:getjson("/api/managed-upgrade/status/"+tid)["Task"]["State"]=="succeeded" and direct_reports("v0.30.03"),180)
                        direct_ledger=lambda:json.loads(sudo("cat",ledger_path,check=True,stdout=subprocess.PIPE).stdout)
                        wait("Direct successful receipt persisted",lambda:direct_ledger()["result_reported"],30)
                        seq=direct_ledger()["highest_sequence"]+1
                        binary_digest=sudo("sha256sum","/opt/nezha/agent/nezha-agent",check=True,stdout=subprocess.PIPE,text=True).stdout.split()[0]
                        # Ed25519 PKCS8 DER from the isolated Dashboard seed,
                        # used only for a signed non-executable regression case.
                        der=work/"ci-local.der"
                        der.write_bytes(bytes.fromhex("302e020100300506032b657004220420")+seed)
                        der.chmod(0o600)
                        local_key=work/"ci-local.pem"
                        subprocess.run(["openssl","pkey","-inform","DER","-in",str(der),"-out",str(local_key)],check=True)
                        local_key.chmod(0o600)
                        bad=work/"not-executable"
                        bad.write_bytes(b"invalid ELF candidate")
                        package_path=work/"direct-bad.nzu"
                        subprocess.run([str(BIN/"agent-package"),"-binary",str(bad),"-out",str(package_path),"-key",str(local_key),"-version","v0.30.04","-min-version","v0.30.03","-os","linux","-arch","amd64","-key-id","dashboard-local","-sequence",str(seq)],check=True)
                        data=package_path.read_bytes()
                        imported=getjson("/api/managed-upgrade/package",data=data,kind="application/octet-stream")
                        bad_id=getjson("/api/managed-upgrade/wave",data={"servers":[direct_sid],"digest":imported["PackageSHA256"],"canary":""})["tasks"][0]["Task"]["ID"]
                        wait("Unexecutable candidate automatically rolled back",lambda:getjson("/api/managed-upgrade/status/"+bad_id)["Task"]["State"]=="rolled_back" and direct_reports("v0.30.03"),180)
                        if sudo("sha256sum","/opt/nezha/agent/nezha-agent",check=True,stdout=subprocess.PIPE,text=True).stdout.split()[0]!=binary_digest:
                            raise RuntimeError("direct rollback did not restore exact previous bytes")
                        wait("Direct rollback receipt persisted",lambda:direct_ledger()["result_reported"],30)
                        if direct_ledger()["highest_sequence"]!=seq:
                            raise RuntimeError("direct rollback lowered sequence")
                        print("Single-binary self-upgrade and failed-exec rollback passed",flush=True)
                    finally:
                        try:
                            sudo("systemctl","disable","--now","nezha-agent.service",stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
                            sudo("rm","-f","/etc/systemd/system/nezha-agent.service",check=True)
                            sudo("systemctl","daemon-reload",check=True)
                            sudo("rm","-rf","--","/opt/nezha",check=True)
                        finally:
                            sudo("chmod",format(original_opt_mode,"04o"),"/opt",check=True)
                return
            stop(dashboard)
            cfg={"enabled":True,"allow_execution":True,"package_dir":str(work/"packages"),"linux_amd64":ARCH=="amd64","linux_arm64":ARCH=="arm64","keys":{"ci":pub},"targets":[{"id":sid,"secret_sha256":hashlib.sha256(secret.encode()).hexdigest(),"arch":ARCH,"initial_sequence":1} for sid,secret in targets]}
            config(work/"upgrade.json",cfg)
            env["NZ_MANAGED_UPGRADE_CONFIG"]=str(work/"upgrade.json")
            dashboard=start("dashboard-managed",args,env)
            client=browser()
            wait("Managed Dashboard restart",up,60)
            wait("Managed loopback listeners",loopback_listeners,30)
            login()
            if 'onclick="managedUpgrade()"' not in request("/server").decode():
                raise RuntimeError("managed toolbar not enabled")
            request("/api/managed-upgrade/wave",{"servers":[targets[0][0]],"digest":"a"*64},origin="https://invalid.example",expected=403)
            dirs={}
            for sid,secret in targets:
                root=work/f"agent-{sid}"
                root.mkdir(mode=0o700)
                dirs[sid]=root
                for name in ("install","state","stage"):
                    (root/name).mkdir(mode=0o700)
                shutil.copyfile(BIN/"agent-v0.30.00",root/"install/nezha-agent")
                (root/"install/nezha-agent").chmod(0o700)
                config(root/"agent.json",{"server":"127.0.0.1:15443","client_secret":secret,"ca_file":str(work/"tls.pem"),"enable_ip_query":False,"report_interval_seconds":1})
                config(root/"supervisor.json",{"enabled":True,"server_id":sid,"initial_sequence":1,"initial_version":"v0.30.00","agent_config":str(root/"agent.json"),"install_dir":str(root/"install"),"state_dir":str(root/"state"),"stage_dir":str(root/"stage"),"keys":{"ci":pub}})
                subprocess.run([str(BIN/"supervisor"),"--config",str(root/"supervisor.json"),"--initialize"],check=True,cwd=work)
                supervisors[sid] = start(f"supervisor-{sid}",[BIN/"supervisor","--config",root/"supervisor.json"])
            def version(sid,v):
                result=getjson("/api/v1/server/details?id="+str(sid))["result"]
                row = result[0] if result else {}
                host = row.get("host") or {}
                recent = 0 < time.time()-row.get("last_active",0) < 90
                observed[sid] = {"expected_version":v,"version_matches":host.get("Version")==v,"recent_report":recent,"has_state":bool(row.get("status"))}
                if supervisors[sid].poll() is not None:
                    raise RuntimeError(f"supervisor {sid} exited with {supervisors[sid].returncode}")
                return recent and host.get("Version")==v and bool(row.get("status"))
            wait("All initial Agent reports",lambda:all(version(sid,"v0.30.00") for sid,_ in targets),120)
            def package(version_label,actual,sequence,minimum):
                path=work/(version_label+".nzu")
                subprocess.run([str(BIN/"agent-package"),"-binary",str(BIN/("agent-"+actual)),"-out",str(path),"-key",str(work/"sign.pem"),"-version",version_label,"-min-version",minimum,"-os","linux","-arch",ARCH,"-key-id","ci","-sequence",str(sequence)],check=True)
                body=path.read_bytes()
                result=getjson("/api/managed-upgrade/package",data=body,kind="application/octet-stream")
                digest=hashlib.sha256(body).hexdigest()
                if result["PackageSHA256"]!=digest:
                    raise RuntimeError("package digest mismatch")
                return digest
            def wave(ids,digest,canary=""):
                return [r["Task"]["ID"] for r in getjson("/api/managed-upgrade/wave",data={"servers":ids,"digest":digest,"canary":canary})["tasks"]]
            def result(tid,sid,state,installed,seq):
                remote=getjson("/api/managed-upgrade/status/"+tid)["Task"]
                if remote["State"] not in ("pending","transferring","verified","installing",state):
                    raise RuntimeError("unexpected terminal state "+remote["State"])
                if remote["State"]!=state:
                    return False
                ledger=json.loads((dirs[sid]/"state/upgrade-state.json").read_text())
                if ledger["current_version"]!=installed or ledger["highest_sequence"]!=seq or not ledger["result_reported"]:
                    return False
                if hashlib.sha256((dirs[sid]/"install/nezha-agent").read_bytes()).digest()!=hashlib.sha256((BIN/("agent-"+installed)).read_bytes()).digest():
                    raise RuntimeError("installed binary mismatch")
                return version(sid,installed)
            ids=[sid for sid,_ in targets]
            digest=package("v0.30.01","v0.30.01",2,"v0.30.00")
            request("/api/managed-upgrade/wave",{"servers":ids[1:],"digest":digest,"canary":""},expected=409)
            canary=wave(ids[:1],digest)[0]
            wait("Signed canary upgrade",lambda:result(canary,ids[0],"succeeded","v0.30.01",2))
            batch=wave(ids[1:],digest,canary)
            wait("Two-target approved wave",lambda:all(result(t,s,"succeeded","v0.30.01",2) for t,s in zip(batch,ids[1:])))
            bad=package("v0.30.02","v0.30.03",3,"v0.30.01")
            rollback=wave(ids[:1],bad)[0]
            wait("Wrong-version signed binary rolls back",lambda:result(rollback,ids[0],"rolled_back","v0.30.01",3))
            request("/api/managed-upgrade/wave",{"servers":ids[:1],"digest":bad,"canary":""},expected=409)
            # Restart both real services after the rollback and verify that
            # neither the Dashboard nor the Agent loses the consumed sequence.
            stop(supervisors[ids[0]])
            stop(dashboard)
            dashboard=start("dashboard-reopened",args,env)
            client=browser()
            wait("Dashboard reopens durable state",up,60)
            login()
            root=dirs[ids[0]]
            supervisors[ids[0]]=start(f"supervisor-reopened-{ids[0]}",[BIN/"supervisor","--config",root/"supervisor.json"])
            wait("Restart retains rollback outcome",lambda:result(rollback,ids[0],"rolled_back","v0.30.01",3),120)
            request("/api/managed-upgrade/wave",{"servers":ids[:1],"digest":bad,"canary":""},expected=409)
            # Pausing execution must preserve administrator status visibility.
            stop(dashboard)
            cfg["allow_execution"]=False
            config(work/"upgrade.json",cfg)
            dashboard=start("dashboard-paused",args,env)
            client=browser()
            wait("Paused Dashboard restart",up,60)
            login()
            paused=getjson("/api/managed-upgrade/status/"+rollback)
            if paused["Task"]["State"]!="rolled_back":
                raise RuntimeError("paused status lost")
            request("/api/managed-upgrade/wave",{"servers":ids[:1],"digest":bad,"canary":""},expected=503)
            request("/api/managed-upgrade/reconcile/"+rollback,{"revision":paused["Revision"],"digest":bad,"confirmed":True},origin="https://invalid.example",expected=403)
            print("Paused status and reconciliation origin guard: passed",flush=True)
            print("Native joint managed upgrade passed: "+ARCH,flush=True)
        except Exception:
            # Report only bounded non-secret diagnostic fields. Never print
            # raw HTTP headers, body, credentials, or full process logs.
            print("Observed Agent status:",json.dumps(observed),flush=True)
            print("Supervisor exit codes:",{sid:p.poll() for sid,p in supervisors.items()},flush=True)
            log = work/"joint-status.log"
            if log.exists():
                from collections import Counter
                counts=Counter(line.strip() for line in log.read_text(errors="replace").splitlines() if re.fullmatch(r"[0-9 -]+",line.strip()))
                print("Proxy port/HTTP/upstream/gRPC status counts:",dict(counts),flush=True)
            raise
        finally:
            for p in reversed(children):
                stop(p)
            # Do not publish private fixture config, credentials, signing key
            # or raw process logs. CI failures report the exact failed phase.

if __name__ == "__main__":
    run()
