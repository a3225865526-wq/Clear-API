import concurrent.futures,hashlib,importlib.util,json,pathlib,subprocess,time,tarfile,shutil,os
SOURCE=pathlib.Path(__file__).parent
R=pathlib.Path(__import__('os').environ.get('CLEARAPI_SIX_LAB_RUNTIME','D:/AI/API/runtime/six-service-final-20261031'))
R.mkdir(parents=True,exist_ok=True)
spec=importlib.util.spec_from_file_location('lab',SOURCE/'prepare-lab.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
W=m.W;E=W/'evidence/new-vps-six-service-20261031/final-install'
SSH=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','IdentitiesOnly=yes','-o','UserKnownHostsFile='+str(R/'known_hosts'),'-i',str(R/'id_ed25519'),'-p','18962','cleardeploy@127.0.0.1']
def remote(arg):return m.run(SSH+['sudo -n /opt/clearapi/bin/clearapi-local-deploy.py '+arg])
def state():return json.loads(m.ex('cat','/opt/clearapi/stack/state.json'))
def dc(*args):
    s=state();return m.ex('docker','compose','-p','clearapi-six-final','--env-file',s['env_file'],'-f',s['release']+'/compose.yaml',*args)
def pg(query,sub=False,db=None):
    svc,user,name=('sub2api-postgres','sub2api','sub2api') if sub else ('gateway-postgres','clearapi','clearapi')
    return dc('exec','-T',svc,'psql','-U',user,'-d',db or name,'-Atc',query).decode().strip()
def ledger():return json.loads(pg("SELECT json_build_object('quota',(SELECT quota FROM users WHERE id=1),'token_used',(SELECT coalesce(sum(used_quota),0) FROM tokens),'applied',(SELECT coalesce(sum(applied),0) FROM billing_reservations),'reservations',(SELECT count(*) FROM billing_reservations),'logs',(SELECT count(*) FROM logs WHERE type=2),'unique_logs',(SELECT count(distinct request_id) FROM logs WHERE type=2),'log_quota',(SELECT coalesce(sum(quota),0) FROM logs WHERE type=2),'pending_outbox',(SELECT count(*) FROM billing_completion_outboxes WHERE state <> 'delivered'))"))
def save(name,obj):(E/(name+'.json')).write_text(json.dumps(obj,ensure_ascii=False,indent=2),encoding='utf-8')
def api(base,method,path,payload=None,token=None,raw=False):
    edge=dc('ps','-q','edge').decode().strip()
    args=['docker','exec',edge,'curl','-fsS','--max-time','40','-X',method,'-H','Content-Type: application/json']
    if token:args+=['-H','Authorization: Bearer '+token]
    if payload is not None:args+=['--data-binary',json.dumps(payload)]
    data=m.ex(*args,base+path)
    if raw:return data
    obj=json.loads(data)
    if obj.get('success') is False or obj.get('code',0) not in [0,200]:
        (R/'private-api-error.json').write_text(json.dumps(obj));raise RuntimeError('API rejected '+path+'; private diagnostic saved')
    return obj.get('data',obj)
def sample():
    return {'cgroup':m.ex('sh','-c','cat /sys/fs/cgroup/memory.current; cat /sys/fs/cgroup/memory.peak; cat /sys/fs/cgroup/memory.events; cat /sys/fs/cgroup/memory.events.local; cat /sys/fs/cgroup/cpu.stat').decode(),
            'services':[json.loads(l) for l in m.ex('docker','stats','--no-stream','--format','{{json .}}').decode().splitlines()],
            'disk':m.ex('df','-B1','/vps').decode()}
def approve(archive):
    cfg=json.loads(m.ex('cat','/opt/clearapi/config/stack-target.json'));cfg.update(bundle_sha256=m.sha(archive),bundle_bytes=archive.stat().st_size)
    (R/'stack-target.json').write_text(json.dumps(cfg,indent=2));m.put(R/'stack-target.json','/opt/clearapi/config/stack-target.json');m.ex('chmod','600','/opt/clearapi/config/stack-target.json')
    cfgwin=json.loads((R/'windows.json').read_text());cfgwin.update(bundlePath=str(archive),bundleSha256=cfg['bundle_sha256'],remoteConfigSha256=m.sha(R/'stack-target.json'))
    (R/'windows.json').write_text(json.dumps(cfgwin,indent=2))
def deploy(label):
    p=subprocess.run(['pwsh','-NoProfile','-File',str(W/'scripts/deploy-local.ps1'),'-StackTarget','-ConfigPath',str(R/'windows.json')],capture_output=True,text=True,encoding='utf-8')
    if p.returncode:(R/(label+'.private-error')).write_text(p.stderr);raise RuntimeError(label+' failed; private diagnostic saved')
    value=json.loads(p.stdout);save(label,value);return value
def mock_package():
    source=R/'package';dest=R/'mock-package'
    if (R/'mock-package.tar').exists():return R/'mock-package.tar'
    dest.mkdir()
    for p in source.iterdir():
        if p.is_file():os.link(p,dest/p.name)
    (dest/'nginx.conf').unlink()
    ordinary=json.dumps({'id':'fixture','object':'chat.completion','model':'local-mock','choices':[{'index':0,'message':{'role':'assistant','content':'local mock'},'finish_reason':'stop'}],'usage':{'prompt_tokens':10,'completion_tokens':2,'total_tokens':12}},separators=(',',':'))
    chunks=[{'id':'sse-fixture','object':'chat.completion.chunk','choices':[{'index':0,'delta':{'content':'ok'},'finish_reason':None}]},
            {'id':'sse-fixture','object':'chat.completion.chunk','choices':[{'index':0,'delta':{},'finish_reason':'stop'}]},
            {'id':'sse-fixture','object':'chat.completion.chunk','choices':[],'usage':{'prompt_tokens':10,'completion_tokens':2,'total_tokens':12}}]
    stream=''.join('data: '+json.dumps(c,separators=(',',':'))+'\n\n' for c in chunks)+'data: [DONE]\n\n'
    fixture=' server { listen 8090; location /sse/ { default_type text/event-stream; limit_rate 64; return 200 \''+stream.replace('\n','\\n')+'\'; } location / { default_type application/json; return 200 \''+ordinary+'\'; } }\n'
    text=(source/'nginx.conf').read_text().replace('  server {',fixture+'  server {',1)
    (dest/'nginx.conf').write_text(text,newline='\n')
    (dest/'SHA256.json').unlink();hashes={p.name:m.sha(p) for p in dest.iterdir() if p.is_file()};(dest/'SHA256.json').write_text(json.dumps(hashes,indent=2))
    archive=R/'mock-package.tar'
    with tarfile.open(archive,'w:') as out:
        for p in sorted(dest.iterdir()):out.add(p,arcname=p.name)
    return archive
def business():
    initial={'ledger':ledger(),'active_pool_channels':int(pg('SELECT count(*) FROM channels WHERE type=59 AND status=1')),
             'sub2api_accounts':int(pg('SELECT count(*) FROM accounts',True)),'sub2api_keys':int(pg('SELECT count(*) FROM api_keys',True)),'resource':sample()}
    assert initial['active_pool_channels']==initial['sub2api_accounts']==initial['sub2api_keys']==0
    save('initial-default-closed',initial)
    a=mock_package();approve(a);deploy('mock-fixture-deployment')
    source=pathlib.Path('D:/AI/API/runtime/sub2api-integration-20261030')
    old=json.loads((source/'acceptance-state.json').read_text());env=dict(l.split('=',1) for l in (source/'test.env').read_text().splitlines() if '=' in l)
    dump=m.run(['docker','exec','clearapi-sub2api-isolated-20261030-postgres-1','pg_dump','-U','sub2test','-d','sub2test','-Fc'])
    (R/'private-synthetic-sub2api.dump').write_bytes(dump)
    dc('stop','sub2api')
    pg('ALTER DATABASE sub2api RENAME TO sub2api_before_mock',True,db='postgres')
    dc('exec','-T','sub2api-postgres','createdb','-U','sub2api','sub2api')
    s=state();cmd=['docker','exec','-i',m.LAB,'docker','compose','-p','clearapi-six-final','--env-file',s['env_file'],'-f',s['release']+'/compose.yaml','exec','-T','sub2api-postgres','pg_restore','-U','sub2api','-d','sub2api','--no-owner','--no-acl']
    m.run(cmd,input=dump);dc('up','-d','--no-deps','--wait','--wait-timeout','120','sub2api')
    base='http://clearapi-sub2api:8080/api/v1';gateway='http://127.0.0.1:8080'
    admin=api(base,'POST','/auth/login',{'email':'sub2-test@example.invalid','password':env['SUB2_TEST_ADMIN_PASSWORD']})['access_token']
    gate=api(base,'GET','/admin/compliance',token=admin);assert not gate['required']
    user=api(base,'POST','/auth/login',{'email':'clearapi-executor@example.invalid','password':old['sub_user_password']})['access_token']
    api(base,'PUT','/keys/'+str(old['keys']['openai']['id']),{'status':'active'},user)
    api(base,'PUT','/admin/accounts/'+str(old['accounts']['openai']),{'credentials':{'base_url':'http://edge:8090/openai','api_key':'synthetic-unused','model_mapping':{'mock-chat':'mock-chat'}},'concurrency':4},admin)
    api(base,'POST','/admin/accounts/'+str(old['accounts']['openai'])+'/schedulable',{'schedulable':True},admin)
    root=api(gateway,'POST','/api/user/login',{'username':'sixroot','password':(R/'admin-password').read_text()})['access_token']
    models=['six-ordinary','six-pool','six-sse']
    snap=api(gateway,'GET','/api/option/model_pricing?'+ '&'.join('model='+n for n in models),token=root)
    api(gateway,'PATCH','/api/option/model_pricing',{'changes':[{'model_name':n,'expected_version':snap['empty_version'],'pricing':{'billing_setting.billing_mode':'tiered_expr','billing_setting.billing_expr':'tier("fixture",p*2+c*4)'}} for n in models]},root)
    for key,value in [('RetryTimes','0'),('quota_setting.trust_quota_usd','0')]:api(gateway,'PUT','/api/option/',{'key':key,'value':value},root)
    pg('UPDATE users SET quota=1000000 WHERE id=1')
    for name,kind,url,secret,model in [('six-ordinary',1,'http://edge:8090','synthetic-unused','six-ordinary'),('six-pool',59,'http://clearapi-sub2api:8080',old['keys']['openai']['key'],'mock-chat'),('six-sse',1,'http://edge:8090/sse','synthetic-unused','six-sse')]:
        api(gateway,'POST','/api/channel/',{'mode':'single','channel':{'name':name,'type':kind,'status':1,'group':'default','models':name,'base_url':url,'key':secret,'model_mapping':json.dumps({name:model}),'auto_ban':0}},root)
    api(gateway,'POST','/api/token/',{'name':'six-local-only','remain_quota':100000,'unlimited_quota':False,'expired_time':-1,'group':'default'},root)
    tid=int(pg("SELECT id FROM tokens WHERE name='six-local-only'"));key=api(gateway,'POST',f'/api/token/{tid}/key',{},root)['key']
    private={'root_token':root,'relay_key':key,'sub_admin':admin,'sub_user':user,'internal_key_id':old['keys']['openai']['id'],'account_id':old['accounts']['openai'],'token_id':tid}
    (R/'private-business.json').write_text(json.dumps(private))
    before_usage=int(pg('SELECT count(*) FROM usage_logs',True));sub_balances=pg('SELECT id,balance FROM users ORDER BY id',True)
    out={'cases':[],'samples':[],'mock_origin':'restored acknowledged synthetic fixture; no compliance acceptance or real provider'}
    def request(n,stream=False):
        start=time.monotonic();raw=api(gateway,'POST','/v1/chat/completions',{'model':n,'messages':[{'role':'user','content':'local only'}],'max_tokens':16,'stream':stream,'stream_options':{'include_usage':True}} if stream else {'model':n,'messages':[{'role':'user','content':'local only'}],'max_tokens':16},key,raw=True)
        if stream:assert b'[DONE]' in raw and b'local' not in raw
        else:assert json.loads(raw)['choices'][0]['message']['content']=='local mock'
        return {'model':n,'stream':stream,'seconds_including_driver':time.monotonic()-start,'body_sha256':hashlib.sha256(raw).hexdigest()}
    for name,count in [('six-ordinary',4),('six-pool',4),('six-sse',2)]:
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:out['cases']+=list(pool.map(lambda _:request(name,name=='six-sse'),range(count)))
        out['samples'].append(sample())
    for _ in range(30):
        facts=ledger()
        if facts['pending_outbox']==0 and facts['logs']==10:break
        time.sleep(1)
    assert facts['quota']==999860 and facts['applied']==facts['token_used']==facts['log_quota']==140,facts
    assert facts['logs']==facts['unique_logs']==10 and facts['pending_outbox']==0,facts
    out['ledger']=facts;out['sub2_usage_delta']=int(pg('SELECT count(*) FROM usage_logs',True))-before_usage
    assert out['sub2_usage_delta']==4
    assert pg('SELECT id,balance FROM users ORDER BY id',True)==sub_balances
    out['sub2_wallets_unchanged']=True
    for row in json.loads(pg("SELECT coalesce(json_agg(x),'[]') FROM (SELECT id FROM channels) x")):
        api(gateway,'POST','/api/channel/'+str(row['id'])+'/status',{'status':2},root)
    api(base,'PUT','/keys/'+str(private['internal_key_id']),{'status':'inactive'},user)
    api(base,'POST','/admin/accounts/'+str(private['account_id'])+'/schedulable',{'schedulable':False},admin)
    save('business',out);print('ordinary4 pool4 SSE2 + exact wallet140 PASS',flush=True)
def restore():
    before=ledger()
    # Persist non-empty expiring Redis data, then prove real RDB -> AOF restoration.
    dc('exec','-T','sub2api-redis','redis-cli','SET','six-recovery-fixture','persisted','EX','3600')
    proof=json.loads(remote('stack-backup --keep-fenced'))
    bid=proof['backup_id'];cipher='/opt/clearapi/stack/backups/'+bid+'/snapshot.tar.gpg'
    m.ex('gpg','--batch','--yes','--output','/opt/clearapi/config/stack-restore.tar','--decrypt',cipher)
    m.ex('chmod','600','/opt/clearapi/config/stack-restore.tar')
    plaintext_sha=m.ex('sha256sum','/opt/clearapi/config/stack-restore.tar').decode().split()[0]
    approval={'target_id':'clearapi-six-final','backup_id':bid,'plaintext_sha256':plaintext_sha,'observed_data_digest':proof['data_digest'],'approved_by':'local-isolated-test-only'}
    (R/'restore-approval.json').write_text(json.dumps(approval));m.put(R/'restore-approval.json','/opt/clearapi/config/stack-recovery.json');m.ex('chmod','600','/opt/clearapi/config/stack-recovery.json')
    restored=json.loads(remote('stack-recover --backup-id '+bid))
    assert restored['original_data_preserved'] and ledger()==before
    assert dc('exec','-T','sub2api-redis','redis-cli','GET','six-recovery-fixture').decode().strip()=='persisted'
    ttl=int(dc('exec','-T','sub2api-redis','redis-cli','TTL','six-recovery-fixture'));assert 0<ttl<3600
    result={'fresh_encrypted_backup':proof,'restore':restored,'ledger_before':before,'ledger_after':ledger(),'redis_value_and_ttl_restored':True,'redis_ttl':ttl,'samples':[sample()]}
    for round in range(2):
        dc('restart','new-api','sub2api','sub2api-redis');dc('up','-d','--wait','--wait-timeout','150')
        assert ledger()==before
        result['samples'].append(sample())
    result['two_restarts_ledger_unchanged']=True
    # A post-restore request/token must receive a fresh ID, not collide with sequences.
    save('backup-restore-restart',result);print('encrypted backup + two PG schemas/data + Redis RDB/AOF/TTL + restarts PASS',flush=True)
if __name__=='__main__':
    import sys
    for phase in sys.argv[1:]:{'business':business,'restore':restore}[phase]()
