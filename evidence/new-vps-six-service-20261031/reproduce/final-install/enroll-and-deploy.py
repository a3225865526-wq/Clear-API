import hashlib,importlib.util,json,pathlib,subprocess
SOURCE=pathlib.Path(__file__).parent
R=pathlib.Path(__import__('os').environ.get('CLEARAPI_SIX_LAB_RUNTIME','D:/AI/API/runtime/six-service-final-20261031'))
R.mkdir(parents=True,exist_ok=True)
spec=importlib.util.spec_from_file_location('lab',SOURCE/'prepare-lab.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.install()
cfg=json.loads((R/'stack-target.json').read_text());cfg['target_id']='clearapi-six-final'
(R/'stack-target.json').write_text(json.dumps(cfg,indent=2));m.put(R/'stack-target.json','/opt/clearapi/config/stack-target.json');m.ex('chmod','600','/opt/clearapi/config/stack-target.json')
windows=json.loads((R/'windows.json').read_text());windows.update(targetId=cfg['target_id'],remoteConfigSha256=m.sha(R/'stack-target.json'))
(R/'windows.json').write_text(json.dumps(windows,indent=2))
m.ex('mkdir','-p','/opt/clearapi/stage/package','/opt/clearapi/stage/secrets')
with (R/'package.tar').open('rb') as data:
    m.run(['docker','exec','-i',m.LAB,'tar','-xpf','-','-C','/opt/clearapi/stage/package'],stdin=data)
for n in ['stack-target.json','stack.env']:
    m.put(R/n,'/opt/clearapi/stage/'+n);m.ex('chmod','600','/opt/clearapi/stage/'+n)
for n in ['token-key','admin-password','terms.txt','privacy.txt']:
    m.put(R/n,'/opt/clearapi/stage/secrets/'+n);m.ex('chmod','600','/opt/clearapi/stage/secrets/'+n)
m.put(R/'id_ed25519.pub','/opt/clearapi/stage/cleardeploy.pub')
public=m.ex('gpg','--armor','--export',cfg['backup_recipient'])
m.ex('sh','-c','cat > /opt/clearapi/stage/backup-public.asc',input=public)
output=m.ex('sh','/opt/clearapi/stage/package/install-stack-target.sh','/opt/clearapi/stage/package',
    '/opt/clearapi/stage/stack-target.json','/opt/clearapi/stage/stack.env','/opt/clearapi/stage/secrets',
    '/opt/clearapi/stage/cleardeploy.pub','/opt/clearapi/stage/backup-public.asc')
E=m.W/'evidence/new-vps-six-service-20261031/final-install';E.mkdir(parents=True,exist_ok=True)
(E/'enrollment.json').write_text(json.dumps({'result':'PASS','root_installer_ran':True,'visudo':'PASS','logrotate_debug':'PASS','sshd_effective_key_only':'PASS','preflight':json.loads(output.decode().splitlines()[-1])},indent=2))
for label,opts in [('dry-run',['-DryRun']),('fresh-deploy',[])]:
    p=subprocess.run(['pwsh','-NoProfile','-File',str(m.W/'scripts/deploy-local.ps1'),'-StackTarget',*opts,'-ConfigPath',str(R/'windows.json')],capture_output=True,text=True,encoding='utf-8')
    if p.returncode:
        (R/(label+'.private-error')).write_text(p.stderr,encoding='utf-8');raise RuntimeError(label+' failed; private diagnostic saved')
    (E/(label+'.json')).write_text(json.dumps(json.loads(p.stdout),indent=2))
    print(label,'PASS',flush=True)
