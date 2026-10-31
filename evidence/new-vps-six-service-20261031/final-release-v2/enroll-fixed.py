import hashlib,importlib.util,json,pathlib,secrets,subprocess,time,os
R=pathlib.Path(__file__).parent
spec=importlib.util.spec_from_file_location('lab',R/'prepare-lab.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
W=m.W;E=W/'evidence/new-vps-six-service-20261031/final-release-v2';P=R/'package'
B=pathlib.Path('D:/AI/API/runtime/six-service-helper-20261031/release-v2.tar')
def save(name,obj):(E/(name+'.json')).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n',encoding='utf-8',newline='\n')
def deploy(name,dry=False):
    p=subprocess.run(['pwsh','-NoProfile','-File',str(W/'scripts/deploy-local.ps1'),'-StackTarget',*(['-DryRun'] if dry else []),'-ConfigPath',str(R/'windows.json')],capture_output=True,text=True,encoding='utf-8',timeout=900)
    if p.returncode:
        (R/(name+'.private-error')).write_text(p.stderr,encoding='utf-8');raise RuntimeError(name+' failed; private error retained')
    obj=json.loads(p.stdout);save(name,obj);print(name,obj['result'],flush=True);return obj
def enroll():
    m.ex('sh','-c','mkdir -p /opt/clearapi/stage/package /opt/clearapi/stage/secrets; chmod 700 /opt/clearapi/stage; id cleardeploy >/dev/null 2>&1 || adduser -D cleardeploy')
    m.ex('sh','-c','echo "cleardeploy:'+secrets.token_hex(24)+'" | chpasswd')
    with B.open('rb') as data:m.run(['docker','exec','-i',m.LAB,'tar','-xpf','-','-C','/opt/clearapi/stage/package'],stdin=data)
    lines=[]
    for l in (P/'production.env.example').read_text().splitlines():
        if '=' not in l or l.startswith('#'):continue
        key,value=l.split('=',1)
        if 'PASSWORD' in key or 'SECRET' in key or key=='SUB2_TOTP_KEY':value=secrets.token_hex(32)
        if key=='SUB2_ADMIN_EMAIL':value='six-release@example.invalid'
        if key=='FINANCE_OWNER_USER_ID':value='1'
        if key=='PUBLIC_ORIGIN':value='https://six-release.example.invalid'
        lines.append(key+'='+value)
    (R/'stack.env').write_text('\n'.join(lines)+'\n',newline='\n')
    contents={'token-key':secrets.token_hex(32),'admin-password':secrets.token_hex(24),
        'terms.txt':'Owner approved isolated deployment fixture terms; no public use',
        'privacy.txt':'Owner approved isolated deployment fixture privacy; no customer data'}
    for name,value in contents.items():(R/name).write_text(value,newline='\n')
    m.ex('gpg','--batch','--passphrase','','--quick-generate-key','Six release <six-release@example.invalid>','rsa2048','encr','0')
    listing=m.ex('gpg','--with-colons','--list-keys','six-release@example.invalid').decode()
    fingerprint=next(x.split(':')[9] for x in listing.splitlines() if x.startswith('fpr:'))
    cfg=json.loads((P/'target.server.example.json').read_text())
    cfg.update(target_id='clearapi-six-release-v2',isolated=True,daemon_id=m.ex('docker','info','--format','{{.ID}}').decode().strip(),
        bundle_sha256=m.sha(B),bundle_bytes=B.stat().st_size,source_archive_sha256=m.sha(P/'gateway-source.tar'),backup_recipient=fingerprint)
    cfg['helper_files']={n:m.sha(P/n) for n in cfg['helper_files']}
    cfg['protected_inputs']={n:m.sha(R/n.removeprefix('secrets/')) for n in cfg['protected_inputs']}
    cfg['bootstrap'].update({'admin-username':'sixroot','terms-version':'isolated-fixture-v1','privacy-version':'isolated-fixture-v1','approved-by':'local-isolated-test-only'})
    (R/'stack-target.json').write_text(json.dumps(cfg,indent=2)+'\n',newline='\n')
    for name in ['stack-target.json','stack.env']:
        m.put(R/name,'/opt/clearapi/stage/'+name);m.ex('chmod','600','/opt/clearapi/stage/'+name)
    for name in contents:
        m.put(R/name,'/opt/clearapi/stage/secrets/'+name);m.ex('chmod','600','/opt/clearapi/stage/secrets/'+name)
    m.run(['ssh-keygen','-q','-t','ed25519','-N','','-f',str(R/'id_ed25519')])
    m.put(R/'id_ed25519.pub','/opt/clearapi/stage/cleardeploy.pub')
    m.ex('sh','-c','cat > /opt/clearapi/stage/backup-public.asc',input=m.ex('gpg','--armor','--export',fingerprint))
    output=m.ex('sh','/opt/clearapi/stage/package/install-stack-target.sh','/opt/clearapi/stage/package','/opt/clearapi/stage/stack-target.json',
        '/opt/clearapi/stage/stack.env','/opt/clearapi/stage/secrets','/opt/clearapi/stage/cleardeploy.pub','/opt/clearapi/stage/backup-public.asc')
    save('enrollment',{'root_installer':'PASS','bundle_sha256':cfg['bundle_sha256'],'preflight':json.loads(output.decode().splitlines()[-1]),'fixture_db_sha256':m.sha(pathlib.Path('D:/AI/API/runtime/six-service-final-20261031/private-synthetic-sub2api.dump'))})
    hostkey=m.ex('cat','/etc/ssh/ssh_host_ed25519_key.pub').decode().strip()
    (R/'known_hosts').write_text('[127.0.0.1]:18964 '+hostkey+'\n',newline='\n')
    windows=json.loads((P/'target.windows.example.json').read_text())
    windows.update(targetId=cfg['target_id'],host='127.0.0.1',port=18964,isolated=True,identityFile=str(R/'id_ed25519'),knownHostsFile=str(R/'known_hosts'),
        bundlePath=str(B),bundleSha256=cfg['bundle_sha256'],remoteConfigSha256=m.sha(R/'stack-target.json'))
    (R/'windows.json').write_text(json.dumps(windows,indent=2)+'\n',newline='\n')
    deploy('dry-run',True);deploy('fresh-fixed-package');deploy('repeat-fixed-package')
if __name__=='__main__':enroll()
