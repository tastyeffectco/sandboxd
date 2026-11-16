# A one-page web front for Sherlock (github.com/sherlock-project/sherlock):
# type a username, see the sites where it exists. Standard library only.
import os, re, subprocess
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

SHERLOCK = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".venv", "bin", "sherlock")
VALID = re.compile(r"^[A-Za-z0-9._-]{1,40}$")
FOUND = re.compile(r"^\[\+\]\s*([^:]+):\s*(https?://\S+)")

PAGE = """<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sherlock</title><style>
body{font-family:system-ui,sans-serif;max-width:720px;margin:40px auto;padding:0 16px;color:#1f2933;background:#fafaf7}
h1{margin:0 0 4px}p.sub{color:#52606d;margin:0 0 24px}
form{display:flex;gap:8px}input{flex:1;font-size:18px;padding:10px 12px;border:1px solid #cbd2d9;border-radius:8px}
button{font-size:16px;padding:10px 18px;border:0;border-radius:8px;background:#1f6f5c;color:#fff;cursor:pointer}
button:disabled{opacity:.6}#status{margin:18px 0 8px;color:#52606d}
ul{list-style:none;padding:0}li{padding:8px 10px;border-bottom:1px solid #e4e7eb;display:flex;justify-content:space-between;gap:12px}
li a{color:#1f6f5c;word-break:break-all}small{color:#7b8794}
</style></head><body>
<h1>Sherlock</h1><p class="sub">Find where a username is used, across 400+ websites. Try your own username first.</p>
<form id="f"><input id="u" placeholder="A username, for example: johndoe" autocomplete="off" required>
<button id="b">Search</button></form><div id="status"></div><ul id="r"></ul>
<p><small>Uses the open-source Sherlock project. Only search for people who agree to it.</small></p>
<script>
const f=document.getElementById('f'),u=document.getElementById('u'),b=document.getElementById('b'),s=document.getElementById('status'),r=document.getElementById('r');
f.onsubmit=async e=>{e.preventDefault();const name=u.value.trim();if(!/^[A-Za-z0-9._-]{1,40}$/.test(name)){s.textContent='Use letters, numbers, dots, dashes or underscores.';return}
b.disabled=true;r.innerHTML='';let n=0;s.textContent='Searching... this takes about a minute.';
try{const res=await fetch('search?u='+encodeURIComponent(name));const rd=res.body.getReader();const dec=new TextDecoder();let buf='';
for(;;){const {done,value}=await rd.read();if(done)break;buf+=dec.decode(value,{stream:true});let i;
while((i=buf.indexOf('\\n'))>=0){const line=buf.slice(0,i);buf=buf.slice(i+1);if(!line)continue;const [site,url]=line.split('\\t');
if(site==='#error'){s.textContent=url;continue}n++;const li=document.createElement('li');const a=document.createElement('a');a.href=url;a.target='_blank';a.rel='noreferrer';a.textContent=url;
const t=document.createElement('span');t.textContent=site;li.append(t,a);r.append(li);s.textContent='Found on '+n+' sites so far...'}}
s.textContent=n?'Done: found on '+n+' sites.':'Done: not found on any site.'}catch(err){s.textContent='The search stopped. Try again.'}
b.disabled=false}
</script></body></html>"""


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def do_GET(self):
        url = urlparse(self.path)
        if url.path.endswith("/search"):
            return self.search(parse_qs(url.query).get("u", [""])[0])
        body = PAGE.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def search(self, name):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if not VALID.match(name):
            self.wfile.write(b"#error\tUse letters, numbers, dots, dashes or underscores.\n")
            return
        cmd = [SHERLOCK, name, "--print-found", "--no-color", "--timeout", "15"]
        with subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, cwd="/tmp") as p:
            try:
                for line in p.stdout:
                    m = FOUND.match(line.strip())
                    if m:
                        self.wfile.write(f"{m.group(1).strip()}\t{m.group(2)}\n".encode())
                        self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                p.kill()

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "3000"))), Handler).serve_forever()
