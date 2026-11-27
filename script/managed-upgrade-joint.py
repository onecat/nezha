"""Isolated native integration; no production secrets or deployment."""
import hashlib
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
            write(work/"dashboard.yaml", "httpport: 18008\ngrpcport: 15555\nlanguage: en-US\nlocalauth:\n  username: smoke\n  passwordhash: "+json.dumps(hashed)+"\n")
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
  access_log off;
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
            dashboard=start("dashboard",args,env)
            def up():
                try:
                    return b'name="csrf_token"' in request("/login") 
                except RuntimeError:
                    return False
            wait("Dashboard login",up,60)
            login()
            request("/api/managed-upgrade/wave",{"servers":[1],"digest":"a"*64},expected=503)
            targets=[]
            for i in range(3):
                name=f"joint-agent-{i}"
                reg=getjson("/api/v1/server/register",data={"Name":name,"Tag":"joint","HideForGuest":"on"})
                secret=reg["secret"]
                listing=getjson("/api/v1/server/list")["result"]
                sid=next(row["id"] for row in listing if row["name"]==name)
                targets.append((sid,secret))
            stop(dashboard)
            cfg={"enabled":True,"allow_execution":True,"package_dir":str(work/"packages"),"linux_amd64":ARCH=="amd64","linux_arm64":ARCH=="arm64","keys":{"ci":pub},"targets":[{"id":sid,"secret_sha256":hashlib.sha256(secret.encode()).hexdigest(),"arch":ARCH,"initial_sequence":1} for sid,secret in targets]}
            config(work/"upgrade.json",cfg)
            env["NZ_MANAGED_UPGRADE_CONFIG"]=str(work/"upgrade.json")
            dashboard=start("dashboard-managed",args,env)
            client=browser()
            wait("Managed Dashboard restart",up,60)
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
                start(f"supervisor-{sid}",[BIN/"supervisor","--config",root/"supervisor.json"])
            def version(sid,v):
                result=getjson("/api/v1/server/details?id="+str(sid))["result"]
                return bool(result and result[0].get("last_active") and result[0].get("host",{}).get("Version")==v)
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
            print("Native joint managed upgrade passed: "+ARCH,flush=True)
        finally:
            for p in reversed(children):
                stop(p)
            # Do not publish private fixture config, credentials, signing key
            # or raw process logs. CI failures report the exact failed phase.

if __name__ == "__main__":
    run()
