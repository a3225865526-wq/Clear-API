import concurrent.futures,copy,hashlib,importlib.util,json,os,pathlib,shutil,subprocess,tarfile,time
spec=importlib.util.spec_from_file_location('lab',pathlib.Path(__file__).with_name('prepare-lab.py'))
lab=importlib.util.module_from_spec(spec);spec.loader.exec_module(lab)
R,W,ex,put,run=lab.R,lab.W,lab.ex,lab.put,lab.run
E=W/'evidence/new-vps-six-service-20261031';E.mkdir(parents=True,exist_ok=True)
SSH=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','IdentitiesOnly=yes','-o','UserKnownHostsFile='+str(R/'known_hosts'),'-i',str(R/'id_ed25519'),'-p','18961','cleardeploy@127.0.0.1']
HELPER='sudo -n /opt/clearapi/bin/clearapi-local-deploy.py '
def remote(args,data=None,okay=True):
    p=subprocess.run(SSH+[HELPER+args],input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if okay and p.returncode:raise RuntimeError(p.stderr.decode(errors='replace')[-1500:])
    return p
def record(name,value):
    (E/(name+'.json')).write_text(json.dumps(value,ensure_ascii=False,indent=2))
def current():return json.loads(ex('cat','/opt/clearapi/stack/state.json'))
def dc(*args,state=None):
    s=state or current();return ex('docker','compose','-p','clearapi-six-new','--env-file',s['env_file'],'-f',s['release']+'/compose.yaml',*args)
def cid(name):return dc('ps','-q',name).decode().strip()
def pg(query):return dc('exec','-T','gateway-postgres','psql','-U','clearapi','-d','clearapi','-Atc',query).decode().strip()
def ledger():return pg("SELECT json_build_object('quota',(SELECT quota FROM users WHERE id=1),'token_used',(SELECT sum(used_quota) FROM tokens),'applied',(SELECT sum(applied) FROM billing_reservations),'reservations',(SELECT count(*) FROM billing_reservations),'logs',(SELECT count(*) FROM logs WHERE type=2),'unique_logs',(SELECT count(distinct request_id) FROM logs WHERE type=2),'log_quota',(SELECT sum(quota) FROM logs WHERE type=2),'pending_outbox',(SELECT count(*) FROM billing_completion_outboxes WHERE state <> 'delivered'))")
def refresh():
    run(['python',str(SOURCE/'update-code.py')])
def package(name,edit=None):
    import yaml
    source=R/'package-current';target=R/name
    attempt=0
    while target.exists():
        attempt+=1;target=R/(name+'-retry'+str(attempt))
    target.mkdir(exist_ok=False)
    for p in source.iterdir():
        if p.is_file():os.link(p,target/p.name)
    # Break links before changing a file; never mutate an approved release package.
    for n in ['compose.yaml','SHA256.json','local-deploy-stack.py','local-deploy-vps.py','local-deploy-stack.ps1']:
        raw=(target/n).read_bytes();(target/n).unlink();(target/n).write_bytes(raw)
    for n in ['local-deploy-stack.py','local-deploy-vps.py','local-deploy-stack.ps1']:
        shutil.copy2(W/'scripts'/n,target/n)
    config=yaml.safe_load((target/'compose.yaml').read_text())
    if edit:edit(config,target)
    (target/'compose.yaml').write_text(yaml.safe_dump(config,sort_keys=False),newline='\n')
    manifest={p.name:lab.sha(p) for p in target.iterdir() if p.is_file() and p.name!='SHA256.json'}
    (target/'SHA256.json').write_text(json.dumps(manifest,indent=2)+'\n')
    archive=R/(target.name+'.tar')
    with tarfile.open(archive,'w:') as tar:
        for p in sorted(target.iterdir()):
            if p.is_file():tar.add(p,arcname=p.name)
    return archive
def approve(archive):
    cfg=json.loads(ex('cat','/opt/clearapi/config/stack-target.json'))
    cfg.update(bundle_sha256=lab.sha(archive),bundle_bytes=archive.stat().st_size)
    (R/'stack-target.json').write_text(json.dumps(cfg,indent=2));put(R/'stack-target.json','/opt/clearapi/config/stack-target.json');ex('chmod','600','/opt/clearapi/config/stack-target.json')
    windows=json.loads((R/'windows.json').read_text());windows.update(remoteConfigSha256=lab.sha(R/'stack-target.json'),bundleSha256=cfg['bundle_sha256'],bundlePath=str(archive))
    (R/'windows.json').write_text(json.dumps(windows,indent=2))
    return cfg
def send(archive,okay=True):
    before=json.loads(remote('stack-preflight').stdout)
    with archive.open('rb') as data:
        p=subprocess.run(SSH+[HELPER+f'stack-receive --bundle-sha256 {lab.sha(archive)} --bundle-bytes {archive.stat().st_size} --expected-generation {before["generation"]}'],stdin=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if okay and p.returncode:raise RuntimeError(p.stderr.decode(errors='replace')[-1800:])
    return p
def root_hash():
    script='''import importlib.util,json; s=importlib.util.spec_from_file_location('h','/opt/clearapi/bin/clearapi-local-deploy.py');h=importlib.util.module_from_spec(s);s.loader.exec_module(h);s=importlib.util.spec_from_file_location('m','/opt/clearapi/bin/local-deploy-stack.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);t=m.StackTarget(h);j=t.read(t.journal) if t.journal.exists() else None;st=j['previous'] if j else t.state(); print(t.data_digest(st['release'],st))'''
    return ex('python3','-c',script).decode().strip()
def resume():
    j=json.loads(ex('cat','/opt/clearapi/stack/pending.json'))
    old=j['previous']
    # Bring back only the original DB/Redis engines; keep both applications fenced.
    dc('up','-d','--wait','--wait-timeout','120','gateway-postgres','sub2api-postgres','sub2api-redis',state=old)
    approval={'action':'resume_previous_images','target_id':'clearapi-six-new','candidate_bundle_sha256':j['bundle_sha256'],'observed_data_digest':root_hash(),'approved_by':'local-isolated-test-only'}
    (R/'recovery-approval.json').write_text(json.dumps(approval));put(R/'recovery-approval.json','/opt/clearapi/config/stack-recovery.json');ex('chmod','600','/opt/clearapi/config/stack-recovery.json')
    return remote('stack-recover --resume-previous')
def fault(name,edit,expect_oom=False):
    baseline=ledger();a=package(name,edit);approve(a)
    p=send(a,False)
    assert p.returncode!=0,name
    out={'exit':p.returncode,'error':p.stderr.decode(errors='replace')[-2000:],'ledger_before':json.loads(baseline)}
    record(name+'-attempt',out)
    pending=ex('test','-f','/opt/clearapi/stack/pending.json') if False else None
    j=json.loads(ex('cat','/opt/clearapi/stack/pending.json'))
    out['phase']=j['phase'];out['backup_id']=j.get('backup',{}).get('backup_id')
    if expect_oom:
        info=json.loads(ex('docker','inspect','clearapi-six-new-edge-1'))[0]
        out['oom_killed']=info['State']['OOMKilled'];assert out['oom_killed'],'expected actual service OOM'
    noauto=remote('stack-recover',okay=False)
    out['automatic_recovery_exit']=noauto.returncode
    assert noauto.returncode!=0,'post-switch must not auto restore DB'
    r=resume();out['authorized_resume']=json.loads(r.stdout)
    assert json.loads(ledger())==json.loads(baseline),'wallet/logs changed by failed deployment'
    out['ledger_after']=json.loads(ledger());record(name,out);print(name,'PASS',flush=True)
def scenario_faults():
    refresh()
    def fail_db(c,p):
        s=c['services']['gateway-postgres'];s['command']=['postgres','-c','invalid_setting_for_test=1'];s['restart']='no';s['healthcheck'].update(interval='1s',retries=1,start_period='0s')
    fault('database-start-failure',fail_db)
    def fail_redis(c,p):
        s=c['services']['sub2api-redis'];s['command']=['redis-server','--invalid-setting-test'];s['restart']='no';s['healthcheck'].update(interval='1s',retries=1,start_period='0s')
    fault('redis-start-failure',fail_redis)
    def fail_health(c,p):
        s=c['services']['edge'];s['healthcheck'].update(test=['CMD','false'],interval='1s',retries=1,start_period='0s')
    fault('edge-health-failure',fail_health)
    def fail_migrate(c,p):
        s=c['services']['new-api'];s['entrypoint']=['/finance-migrate'];s['command']=['--dialect','postgres','--apply-approved-finance-v9'];s['environment']['CLEARAPI_FINANCE_MIGRATION_DSN']='postgres://unavailable.invalid/fixture';s['restart']='no';s['healthcheck'].update(interval='1s',retries=1,start_period='0s')
    fault('migration-command-failure',fail_migrate)
def oom():
    refresh()
    def edit(c,p):
        s=c['services']['edge'];s['mem_limit']='12m';s['memswap_limit']='12m';s['restart']='no';s['entrypoint']=['sh','-c']
        s['command']=['exec awk \'BEGIN{x="x";for(i=0;i<28;i++)x=x x;print length(x)}\'']
        s['healthcheck'].update(interval='1s',retries=1,start_period='0s')
    fault('actual-edge-oom',edit,True)
def interrupted_restart():
    refresh()
    def slow(c,p):c['services']['new-api']['healthcheck'].update(test=['CMD-SHELL','sleep 8; wget -q -T 5 -O /dev/null http://127.0.0.1:3000/api/status'],interval='1s',timeout='12s',start_period='0s')
    a=package('ssh-interrupted-restart',slow);approve(a);baseline=json.loads(ledger())
    before=json.loads(remote('stack-preflight').stdout)
    candidate_hash=lab.sha(a)
    with a.open('rb') as stream:
        p=subprocess.Popen(SSH+[HELPER+f'stack-receive --bundle-sha256 {lab.sha(a)} --bundle-bytes {a.stat().st_size} --expected-generation {before["generation"]}'],stdin=stream,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        deadline=time.monotonic()+240
        while True:
            result=subprocess.run(['docker','exec',lab.LAB,'cat','/opt/clearapi/stack/pending.json'],capture_output=True)
            if result.returncode==0:
                j=json.loads(result.stdout)
                if j['bundle_sha256']==candidate_hash and j['phase']=='switching':
                    running=ex('docker','inspect','clearapi-six-new-new-api-1','--format','{{.State.Running}}').decode().strip()
                    if running=='true':break
            if p.poll() is not None:raise RuntimeError('deployment ended before interruption window')
            if time.monotonic()>deadline:raise TimeoutError('interruption window')
            time.sleep(.5)
        p.kill();p.communicate(timeout=30)
    deadline=time.monotonic()+200
    while True:
        probe=remote('stack-preflight',okay=False)
        if probe.returncode==0:break
        if time.monotonic()>deadline:raise TimeoutError('helper did not leave a recoverable transaction')
        time.sleep(2)
    state=json.loads(probe.stdout)
    if state['pending']:
        automatic=remote('stack-recover',okay=False);assert automatic.returncode
        resumed=json.loads(resume().stdout)
    else:
        resumed={'result':'helper_completed_before_disconnect','healthy':state['healthy']}
    assert json.loads(ledger())==baseline
    record('ssh-interrupted-restart',{'client_killed_during_service_restart':True,'state_after_disconnect':state,'recovery':resumed,'ledger_unchanged':True})
    print('SSH restart interruption PASS',flush=True)
def financial_fault():
    refresh()
    a=package('financial-inconsistency');approve(a);before=json.loads(remote('stack-preflight').stdout)
    baseline=json.loads(ledger());original_quota=baseline['quota']
    candidate_hash=lab.sha(a)
    with a.open('rb') as stream:
        p=subprocess.Popen(SSH+[HELPER+f'stack-receive --bundle-sha256 {lab.sha(a)} --bundle-bytes {a.stat().st_size} --expected-generation {before["generation"]}'],stdin=stream,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        deadline=time.monotonic()+240
        while True:
            q=subprocess.run(['docker','exec',lab.LAB,'cat','/opt/clearapi/stack/pending.json'],capture_output=True)
            if q.returncode==0:
                j=json.loads(q.stdout)
                if j['bundle_sha256']==candidate_hash and j['phase']=='switching':break
            if time.monotonic()>deadline:raise TimeoutError('financial injection window')
            time.sleep(.5)
        # Deliberate +1 only to the synthetic lab administrator; no real wallet.
        pg('UPDATE users SET quota=quota+1 WHERE id=1')
        stdout,stderr=p.communicate(timeout=240)
    assert p.returncode and b'FINANCIAL_INCONSISTENCY' in stderr
    try:
        resume();raise AssertionError('unsafe rollback accepted modified wallet')
    except RuntimeError as error:
        assert 'MANUAL_RECONCILIATION_REQUIRED' in str(error)
    assert json.loads(ledger())['quota']==original_quota+1
    # Explicit fixture repair, never a deployment helper refund/adjustment.
    pg(f'UPDATE users SET quota={original_quota} WHERE id=1')
    recovered=json.loads(resume().stdout)
    assert json.loads(ledger())==baseline
    record('financial-inconsistency',{'deployment_exit':p.returncode,'financial_change_detected':True,'edge_fenced':True,'snapshot_not_restored_over_new_wallet':True,'synthetic_fixture_repair_only':True,'recovery':recovered})
    print('financial inconsistency detection PASS',flush=True)
def negatives():
    refresh();results=[]
    base=pathlib.Path(json.loads((R/'windows.json').read_text())['bundlePath'])
    # Each package is independently root-approved; validation must still reject it.
    def bad_digest(c,p):
        lock=json.loads((p/'images.lock.json').read_text());tag=c['services']['edge']['image'];lock[tag]['config_id']='sha256:'+'0'*64
        raw=json.dumps(lock).encode();(p/'images.lock.json').unlink();(p/'images.lock.json').write_bytes(raw)
    a=package('bad-image-digest',bad_digest);approve(a);before=json.loads(remote('stack-preflight').stdout)['generation'];p=send(a,False)
    assert p.returncode and (b'identity mismatch' in p.stderr or b'immutable image tag' in p.stderr);assert json.loads(remote('stack-preflight').stdout)['generation']==before
    results.append({'case':'image-config-digest-mismatch','exit':p.returncode,'stopped_before_switch':True})
    def missing(c,p):
        lock=json.loads((p/'images.lock.json').read_text());del lock[c['services']['edge']['image']];(p/'images.lock.json').unlink();(p/'images.lock.json').write_text(json.dumps(lock))
    a=package('missing-image',missing);approve(a);p=send(a,False);assert p.returncode and b'inventory mismatch' in p.stderr
    results.append({'case':'missing-image','exit':p.returncode,'stopped_before_switch':True})
    # Truncated SSH stream, not a fabricated server-side hook.
    approve(base);pre=json.loads(remote('stack-preflight').stdout)
    cfg=json.loads((R/'windows.json').read_text())
    cmd=f'stack-receive --bundle-sha256 {cfg["bundleSha256"]} --bundle-bytes {base.stat().st_size} --expected-generation {pre["generation"]}'
    p=remote(cmd,data=base.read_bytes()[:4096],okay=False);assert p.returncode and b'truncated SSH upload' in p.stderr
    results.append({'case':'ssh-upload-interrupted','exit':p.returncode,'state_unchanged':json.loads(remote('stack-preflight').stdout)['generation']==pre['generation']})
    corrupted=bytearray(base.read_bytes());corrupted[4096]^=1
    p=remote(cmd,data=bytes(corrupted),okay=False);assert p.returncode and b'checksum mismatch' in p.stderr
    results.append({'case':'corrupt-upload','exit':p.returncode,'state_unchanged':True})
    del corrupted
    # Real ext4 capacity reduced below the configured reserve. No data volumes removed.
    free=int(ex('python3','-c','import shutil;print(shutil.disk_usage("/vps").free)'))
    ex('fallocate','-l',str(free-4*1024**3),'/vps/capacity-fault.bin')
    p=remote('stack-preflight',okay=False);assert p.returncode and b'insufficient disk' in p.stderr
    ex('rm','/vps/capacity-fault.bin');results.append({'case':'insufficient-disk','exit':p.returncode,'actual_free_below_reserve':True})
    cfg=json.loads(ex('cat','/opt/clearapi/config/stack-target.json'));original=copy.deepcopy(cfg);cfg['minimum_available_bytes']=10**15
    (R/'memory-target.json').write_text(json.dumps(cfg));put(R/'memory-target.json','/opt/clearapi/config/stack-target.json');ex('chmod','600','/opt/clearapi/config/stack-target.json')
    p=remote('stack-preflight',okay=False);assert p.returncode and b'insufficient host memory' in p.stderr
    (R/'memory-target.json').write_text(json.dumps(original));put(R/'memory-target.json','/opt/clearapi/config/stack-target.json');ex('chmod','600','/opt/clearapi/config/stack-target.json')
    results.append({'case':'insufficient-memory-preflight','exit':p.returncode,'synthetic_threshold_not_actual_oom':True})
    p=remote('preflight',okay=False);assert p.returncode;results.append({'case':'new-user-cannot-invoke-legacy-mode','exit':p.returncode})
    p=remote('--config-path /tmp/unapproved stack-preflight',okay=False);assert p.returncode;results.append({'case':'sudo-cannot-select-alternate-config','exit':p.returncode})
    record('negative-preflight',results);print('negative preflights PASS',flush=True)
if __name__=='__main__':
    import sys
    actions={'faults':scenario_faults,'negatives':negatives,'oom':oom,'interrupt':interrupted_restart,'financial':financial_fault}
    for phase in sys.argv[1:]:actions[phase]()
