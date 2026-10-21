const fs = require('node:fs/promises')
const path = require('node:path')
const { spawnSync } = require('node:child_process')
const { ROOTFS, download, decode } = require('../../desktop/src/windows-engine.cjs')
async function main() {
  if (process.platform !== 'win32' || !process.env.GITHUB_ACTIONS || !process.env.RUNNER_TEMP) throw new Error('Disposable Windows CI only')
  const directory = await fs.mkdtemp(path.join(process.env.RUNNER_TEMP, 'sandboxd-net-probe-'))
  const name = 'sandboxd-network-probe'
  const wsl = (args, input) => {
    const r = spawnSync('wsl.exe', args, {input, timeout:600000, maxBuffer:2*1024*1024})
    return {code:r.status, stdout:decode(r.stdout || Buffer.alloc(0)), stderr:decode(r.stderr || Buffer.alloc(0))}
  }
  const shell = (script) => wsl(['-d',name,'-u','root','--cd','/','--exec','bash','-s'],script)
  let imported = false
  try {
    console.log('WSL status',wsl(['--status']))
    const config = await fs.readFile(path.join(process.env.USERPROFILE,'.wslconfig'),'utf8').catch(()=>'(default)')
    console.log('Network config',config.split(/\r?\n/).filter(l=>/network|localhost|firewall|ipv6/i.test(l)))
    const archive = path.join(directory,'ubuntu.tar.gz')
    await download('https://cdimage.ubuntu.com/ubuntu-base/releases/24.04/release/ubuntu-base-24.04.5-base-amd64.tar.gz',archive,ROOTFS.x64.sha256,()=>{})
    const result=wsl(['--import',name,path.join(directory,'disk'),archive,'--version','2'])
    console.log('Import',result)
    if(result.code!==0) throw new Error('Import failed')
    imported=true
    const bootstrap=(await fs.readFile('desktop/src/windows-engine-bootstrap.sh','utf8')).split('if [ ! -f /var/lib/sandboxd-desktop/ready ]; then')[0]
    console.log('Prerequisites',shell(bootstrap.replace('ref="$1"','ref="1a860a57b4d0ca27bcebbfd712a7e45e6fa4a6c9"')+'\napt-get install -y python3 >> /var/log/sandboxd-setup.log 2>&1\n'))
    console.log('Start listeners',shell(`mkdir -p /tmp/probe
nohup python3 -m http.server 19090 --bind 127.0.0.1 --directory /tmp/probe >/tmp/probe/python.log 2>&1 </dev/null &
nohup python3 -m http.server 19091 --bind 0.0.0.0 --directory /tmp/probe >/tmp/probe/python2.log 2>&1 </dev/null &
docker run -d --name probe -p 127.0.0.1:9090:80 -p 9091:80 nginx:alpine
sleep 2
curl -s -o /dev/null -w 'Linux engine status: %{http_code}\\n' http://127.0.0.1:9090
cat /proc/net/tcp
`))
    await new Promise(r=>setTimeout(r,5000))
    for(const port of [9090,9091,19090,19091]) {
      try {const r=await fetch('http://127.0.0.1:'+port,{signal:AbortSignal.timeout(5000)}); console.log('Windows',port,r.status)}
      catch(e) {console.log('Windows',port,e.message,e.cause?.code)}
    }
    console.log('Bindings',shell('ps aux | grep -E "docker-proxy|dockerd|python3"\n'))
  } finally {
    if(imported) console.log('Cleanup',wsl(['--unregister',name]).code)
    await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:200})
  }
}
main().catch(e=>{console.error(e);process.exitCode=1})
