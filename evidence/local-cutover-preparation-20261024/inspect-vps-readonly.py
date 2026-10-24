"""One-shot read-only production inventory; no service or file mutations."""
import json,subprocess,hashlib,stat,re,platform
from pathlib import Path
from datetime import datetime,timezone

def command(args,timeout=25):
    try:
        p=subprocess.run(args,capture_output=True,text=True,timeout=timeout)
        return {'exit':p.returncode,'stdout':p.stdout.strip()}
    except subprocess.TimeoutExpired:
        return {'exit':124,'stdout':''}

def output(args,timeout=25):
    p=command(args,timeout)
    if p['exit']: return None
    return p['stdout']

def file_meta(name):
    p=Path(name)
    if not p.exists() and not p.is_symlink(): return {'exists':False}
    s=p.lstat(); r={'exists':True,'uid':s.st_uid,'gid':s.st_gid,'mode':oct(stat.S_IMODE(s.st_mode)), 'bytes':s.st_size,'symlink':p.is_symlink()}
    if stat.S_ISREG(s.st_mode): r['sha256']=hashlib.sha256(p.read_bytes()).hexdigest()
    return r

r={'observed_at':datetime.now(timezone.utc).isoformat(),'mode':'read_only','production_mutations':False,
   'architecture':platform.machine(),'docker_version':output(['docker','version','--format','{{.Server.Version}}']),
   'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip()}
# Docker formats omit all credential-bearing Config.Env and health-log output.
fmt='{"Id":{{json .Id}},"Image":{{json .Image}},"State":{"Running":{{json .State.Running}},"Status":{{json .State.Status}},"ExitCode":{{json .State.ExitCode}},"Error":{{json .State.Error}},"StartedAt":{{json .State.StartedAt}},"FinishedAt":{{json .State.FinishedAt}},"OOMKilled":{{json .State.OOMKilled}},"Health":{{with index .State "Health"}}{{json .Status}}{{else}}null{{end}}},"RestartCount":{{json .RestartCount}},"ImageRef":{{json .Config.Image}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}},"RestartPolicy":{{json .HostConfig.RestartPolicy}},"PortBindings":{{json .HostConfig.PortBindings}},"NetworkMode":{{json .HostConfig.NetworkMode}}}'
r['containers']={}
for name in ['clearapi-new-api-1','clearapi-edge-caddy','clearapi-pool-executor-1','clearapi-postgres-1']:
    text=output(['docker','inspect','--format',fmt,name])
    if text is None: r['containers'][name]={'exists':False}; continue
    c=json.loads(text); c['compose_labels']={k:v for k,v in (c.pop('Labels') or {}).items() if k.startswith('com.docker.compose.')}
    c['mounts']=[{k:m.get(k) for k in ('Type','Source','Destination','RW','Name')} for m in c.pop('Mounts')]
    image=output(['docker','image','inspect','--format','{"Id":{{json .Id}},"RepoDigests":{{json .RepoDigests}},"Architecture":{{json .Architecture}},"Revision":{{with index .Config "Labels"}}{{json (index . "org.opencontainers.image.revision")}}{{else}}null{{end}}}',c['Image']])
    c['image_identity']=json.loads(image) if image else None
    r['containers'][name]=c
allowed=['CLEARAPI_POOL_ENABLED','CLEARAPI_POOL_CLIPROXY_ENABLED','GIN_MODE']
conditions='or '+ ' '.join('(eq (index $v 0) '+json.dumps(k)+')' for k in allowed)
f='{{range .Config.Env}}{{$v := split . "="}}{{if '+conditions+'}}{{println .}}{{end}}{{end}}'
r['gateway_nonsecret_flags']=output(['docker','inspect','--format',f,'clearapi-new-api-1'])
app=Path('/opt/clearapi/app')
r['app_files']={n:file_meta(app/n) for n in ['docker-compose.prod.yml','compose-candidate.yaml','compose-pg03.yaml','release-manifest.json','pg03-target-manifest.json','controlled-launch-state.json']}
m=app/'release-manifest.json'
if m.exists():
    obj=json.loads(m.read_text()); cur=obj.get('releases',{}).get('current',{})
    r['release']={k:cur.get(k) for k in ['status','candidate_commit','image_ref','image_digest','new_api_image_id','pool_mode','deployed_at_utc']}
    attempt=cur.get('deployment_attempt',{});r['release']['attempt']={k:attempt.get(k) for k in ['id','status','workflow_run','started_at_utc','finished_at_utc']}
h=app/'controlled-launch-state.json'
if h.exists():
    obj=json.loads(h.read_text());r['traffic_hold']={k:obj.get(k) for k in ['schema','stage','public_traffic','created_at_utc','updated_at_utc']};r['traffic_hold']['keys']=sorted(obj)
g=r['containers'].get('clearapi-new-api-1',{})
labels=g.get('compose_labels',{});files=labels.get('com.docker.compose.project.config_files','').split(',')
files=[x for x in files if x];directory=labels.get('com.docker.compose.project.working_dir','/opt/clearapi/app')
if files:
    args=['docker','compose','--project-directory',directory]
    for f in files: args+=['-f',f]
    r['compose']={'files':files,'services':command(args+['config','--services']), 'images':command(args+['config','--images']), 'config_hashes':command(args+['config','--hash','*'])}
runtime={m['Source'] for m in g.get('mounts',[]) if m['Destination']=='/run/clearapi'}
runtime.update(['/run/clearapi','/opt/clearapi/runtime','/opt/clearapi/runtime/new-api'])
r['runtime_paths']={p:file_meta(p) for p in sorted(runtime)}
r['runtime_identity']={}
for folder in sorted(runtime):
    path=Path(folder)/'runtime-identity.json'
    if path.exists():
        obj=json.loads(path.read_text());r['runtime_identity'][str(path)]={k:obj.get(k) for k in ['container_id','started_at','docker_image_id','image_digest','oci_revision','generated_at']}
paths=['/opt/clearapi/bin/clearapi-local-deploy.py','/opt/clearapi/config/local-deploy.json','/opt/clearapi/config/local-deploy.enabled','/opt/clearapi/bin/runtime-identity-refresh.py','/opt/clearapi/bin/runtime-boot-recovery.py','/opt/clearapi/bin/production-deploy-v2.sh','/opt/clearapi/bin/pg03-failed-recovery.py','/opt/clearapi/bin/controlled-traffic-hold.py','/opt/clearapi/backups/bin/production-backup.sh','/opt/clearapi/backups/bin/backup-identity.py','/opt/clearapi/backups/bin/pg03-backup-state.py','/opt/clearapi/edge/Caddyfile','/etc/tmpfiles.d/clearapi-runtime.conf','/etc/systemd/system/clearapi-runtime-directory.service','/etc/systemd/system/docker.service.d/clearapi-runtime.conf','/etc/systemd/system/clearapi-runtime-identity.service']
r['assets']={p:file_meta(p) for p in paths}
r['systemd']={}
props=['ActiveState','SubState','Result','UnitFileState','FragmentPath','DropInPaths','Requires','After','Before','ExecMainStatus','NextElapseUSecRealtime','LastTriggerUSec']
for unit in ['docker.service','clearapi-runtime-directory.service','clearapi-runtime-identity.service','clearapi-backup.service','clearapi-backup.timer']:
    q=command(['systemctl','show',unit,*['--property='+x for x in props]])
    r['systemd'][unit]={'exit':q['exit'],'properties':dict(x.split('=',1) for x in q['stdout'].splitlines() if '=' in x)}
base=Path('/opt/clearapi/backups');proofs=sorted((base/'manifests').glob('*.json'),key=lambda p:p.stat().st_mtime,reverse=True)
r['backup_manifests']=[]
for path in proofs[:5]:
    obj=json.loads(path.read_text()); enc=obj.get('encryption',{}).get('encrypted_artifact',{})
    safe={'file':path.name,'file_sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
    safe.update({k:obj.get(k) for k in ['backup_id','created_at_utc','backup_schema_version','backup_identity_schema_version','candidate_commit','new_api_image_id']})
    safe['identity_kind']='stopped_writer_identity' if 'stopped_writer_identity' in obj else 'running_identity' if 'running_identity' in obj else 'none'
    artifact=base/'encrypted'/str(obj.get('backup_id',''))/str(enc.get('name',''))
    safe['encrypted_artifact']={k:enc.get(k) for k in ['name','bytes','sha256']};safe['artifact_exists']=artifact.is_file()
    if path==proofs[0] and artifact.is_file():
        safe['actual_bytes']=artifact.stat().st_size
        with artifact.open('rb') as stream: safe['actual_sha256']=hashlib.file_digest(stream,'sha256').hexdigest()
    r['backup_manifests'].append(safe)
r['postgres_version']=command(['docker','exec','clearapi-postgres-1','postgres','--version'])
r['deployment_locks']=command(['lslocks','--json','--output','PID,COMMAND,PATH'])
if r['deployment_locks']['exit']==0:
    r['deployment_locks']['stdout']=[x for x in json.loads(r['deployment_locks']['stdout']).get('locks',[]) if 'clearapi' in x.get('path','')]
r['listeners']=command(['ss','-lntuH'])
if r['listeners']['exit']==0: r['listeners']['stdout']=[x for x in r['listeners']['stdout'].splitlines() if re.search(r':(?:80|443|3000)\s',x)]
r['local_health']=command(['curl','--noproxy','*','--silent','--output','/dev/null','--max-time','8','--write-out','%{http_code}','http://127.0.0.1:3000/api/status'])
r['origin_https']=command(['curl','--noproxy','*','--silent','--output','/dev/null','--max-time','8','--resolve','www.clear999.com:443:127.0.0.1','--write-out','%{http_code}','https://www.clear999.com/api/status'])
print(json.dumps(r,sort_keys=True))
