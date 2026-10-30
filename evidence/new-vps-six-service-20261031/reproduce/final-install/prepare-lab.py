import hashlib,json,os,pathlib,secrets,shutil,subprocess,tarfile
SOURCE=pathlib.Path(__file__).parent
R=pathlib.Path(__import__('os').environ.get('CLEARAPI_SIX_LAB_RUNTIME','D:/AI/API/runtime/six-service-final-20261031'))
R.mkdir(parents=True,exist_ok=True)
W=pathlib.Path('D:/AI/API/worktrees/clearapi-new-vps-six-service-helper-20261031')
LAB='clearapi-six-helper-final-20261031'
if R.resolve().is_relative_to(W.resolve()):raise RuntimeError('Private lab runtime cannot be inside the repository')
def run(args,**kw):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,**kw)
    if p.returncode:raise RuntimeError('command failed '+str(args[:4])+': '+p.stderr.decode(errors='replace')[-1400:])
    return p.stdout
def ex(*args,**kw):return run(['docker','exec']+(['-i'] if 'input' in kw else [])+[LAB,*args],**kw)
def put(src,dst):
    return ex('sh','-c','cat > '+dst,input=pathlib.Path(src).read_bytes())
def sha(path):return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()
def install():
    ex('sh','-c','mkdir -p /opt/clearapi/bin /opt/clearapi/config/secrets /opt/clearapi/stack/releases /opt/clearapi/stack/incoming /opt/clearapi/stack/backups /opt/clearapi/stack/receipts /opt/clearapi/stack/data/gateway /opt/clearapi/stack/data/logs /opt/clearapi/stack/sub2data/app; chmod 700 /opt/clearapi/config /opt/clearapi/config/secrets /opt/clearapi/stack; chmod 755 /opt/clearapi/bin')
    for name in ['local-deploy-vps.py','local-deploy-stack.py','local-deploy-state.py']:
        dst='clearapi-local-deploy.py' if name=='local-deploy-vps.py' else name
        put(W/'scripts'/name,'/opt/clearapi/bin/'+dst)
        ex('chmod','750','/opt/clearapi/bin/'+dst)
    # Build a package from the previous actual image archive plus current configs.
    pkg=R/'package'
    for name in ['local-deploy-vps.py','local-deploy-stack.py','local-deploy-state.py','deploy-local.ps1','local-deploy-stack.ps1']:
        shutil.copy2(W/'scripts'/name,pkg/name)
    for source in (W/'deploy/sub2api-low-resource').iterdir():
        if source.is_file() and source.name!='images.lock.json':shutil.copy2(source,pkg/source.name)
    (pkg/'operator-source.json').write_text(json.dumps({'operator_commit':'ISOLATED_WORKING_TREE_VERIFICATION','helper_sha256':{n:sha(W/'scripts'/n) for n in ['local-deploy-vps.py','local-deploy-stack.py','local-deploy-state.py']}}))
    hashes={p.name:sha(p) for p in pkg.iterdir() if p.is_file() and p.name!='SHA256.json'}
    (pkg/'SHA256.json').write_text(json.dumps(hashes,indent=2)+'\n')
    bundle=R/'package.tar'
    with tarfile.open(bundle,'w:') as tar:
        for p in sorted(pkg.iterdir()):
            if p.is_file():tar.add(p,arcname=p.name)
    oldenv=dict(l.split('=',1) for l in (R/'stack.env').read_text().splitlines() if '=' in l) if (R/'stack.env').exists() else {}
    lines=[]
    for l in (pkg/'production.env.example').read_text().splitlines():
        if '=' not in l or l.startswith('#'):continue
        k,v=l.split('=',1)
        if 'PASSWORD' in k or 'SECRET' in k or k=='SUB2_TOTP_KEY':v=oldenv.get(k,secrets.token_hex(32))
        if k=='ROOT':v='/opt/clearapi/stack/data'
        if k=='SUB2_ROOT':v='/opt/clearapi/stack/sub2data'
        if k=='SUB2_ADMIN_EMAIL':v='six-lab@example.invalid'
        if k=='FINANCE_OWNER_USER_ID':v='1'
        if k=='PUBLIC_ORIGIN':v='https://six-lab.example.invalid'
        lines.append(k+'='+v)
    (R/'stack.env').write_text('\n'.join(lines)+'\n',newline='\n')
    put(R/'stack.env','/opt/clearapi/config/stack.env');ex('chmod','600','/opt/clearapi/config/stack.env')
    for name,content in [('token-key',secrets.token_hex(32)),('admin-password',secrets.token_hex(24)),('terms.txt','Isolated deployment fixture terms, no public use'),('privacy.txt','Isolated deployment fixture privacy, no customer data')]:
        if not (R/name).exists():(R/name).write_text(content)
        put(R/name,'/opt/clearapi/config/secrets/'+name);ex('chmod','600','/opt/clearapi/config/secrets/'+name)
    try:ex('gpg','--batch','--list-keys','six-lab@example.invalid')
    except RuntimeError:ex('gpg','--batch','--passphrase','','--quick-generate-key','Six lab <six-lab@example.invalid>','rsa2048','encr','0')
    listing=ex('gpg','--with-colons','--list-keys','six-lab@example.invalid').decode()
    fingerprint=next(x.split(':')[9] for x in listing.splitlines() if x.startswith('fpr:'))
    cfg=json.loads((pkg/'target.server.example.json').read_text())
    cfg.update(isolated=True,daemon_id=ex('docker','info','--format','{{.ID}}').decode().strip(),bundle_sha256=sha(bundle),bundle_bytes=bundle.stat().st_size,
               source_archive_sha256=sha(pkg/'gateway-source.tar'),backup_recipient=fingerprint,minimum_available_bytes=512*1024**2)
    cfg['protected_inputs']={n:__import__('hashlib').sha256(ex('cat','/opt/clearapi/config/'+n)).hexdigest() for n in ['stack.env','secrets/token-key','secrets/admin-password','secrets/terms.txt','secrets/privacy.txt']}
    cfg['helper_files']={n:sha(W/'scripts'/n) for n in cfg['helper_files']}
    cfg['bootstrap'].update({'admin-username':'sixroot','terms-version':'isolated-fixture-v1','privacy-version':'isolated-fixture-v1','approved-by':'local-isolated-test-only'})
    (R/'stack-target.json').write_text(json.dumps(cfg,indent=2))
    (R/'stack-target.enabled').write_text('STACK_TARGET_APPROVED_V1:'+cfg['target_id'])
    for n in ['stack-target.json']:
        put(R/n,'/opt/clearapi/config/'+n);ex('chmod','600','/opt/clearapi/config/'+n)
    if not (R/'id_ed25519').exists():run(['ssh-keygen','-q','-t','ed25519','-N','','-f',str(R/'id_ed25519')])
    ex('sh','-c','id cleardeploy >/dev/null 2>&1 || adduser -D cleardeploy; echo "cleardeploy:'+secrets.token_hex(20)+'" | chpasswd; mkdir -p /home/cleardeploy/.ssh; chmod 700 /home/cleardeploy/.ssh')
    run(['docker','cp',str(R/'id_ed25519.pub'),LAB+':/home/cleardeploy/.ssh/authorized_keys']);ex('sh','-c','chown -R cleardeploy:cleardeploy /home/cleardeploy/.ssh; chmod 600 /home/cleardeploy/.ssh/authorized_keys')
    run(['docker','cp',str(pkg/'stack.sudoers.example'),LAB+':/etc/sudoers.d/clearapi-stack']);ex('chmod','440','/etc/sudoers.d/clearapi-stack');ex('visudo','-cf','/etc/sudoers.d/clearapi-stack')
    hostkey=ex('cat','/etc/ssh/ssh_host_ed25519_key.pub').decode().strip()
    (R/'known_hosts').write_text('[127.0.0.1]:18962 '+hostkey+'\n')
    windows=json.loads((pkg/'target.windows.example.json').read_text())
    windows.update(host='127.0.0.1',port=18962,isolated=True,identityFile=str(R/'id_ed25519'),knownHostsFile=str(R/'known_hosts'),bundlePath=str(bundle),bundleSha256=sha(bundle),remoteConfigSha256=sha(R/'stack-target.json'))
    (R/'windows.json').write_text(json.dumps(windows,indent=2))
    print('lab target protected, sudoers checked, package approved',flush=True)
if __name__=='__main__':install()
