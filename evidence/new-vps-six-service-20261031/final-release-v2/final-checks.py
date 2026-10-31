import importlib.util,json,pathlib,subprocess,time
R=pathlib.Path(__file__).parent
s=importlib.util.spec_from_file_location('business',R/'business-and-restore.py');b=importlib.util.module_from_spec(s);s.loader.exec_module(b)
m=b.m
def pre(ok=True):
    p=subprocess.run(b.SSH+['sudo -n /opt/clearapi/bin/clearapi-local-deploy.py stack-preflight'],capture_output=True)
    if ok:
        assert p.returncode==0,p.stderr.decode();return json.loads(p.stdout)
    assert p.returncode!=0
    return p.stderr.decode().strip()
def drift():
    baseline=b.ledger();initial=pre();edge=b.dc('ps','-q','edge').decode().strip();results=[]
    m.ex('docker','update','--memory','48m','--memory-swap','48m',edge)
    try:
        error=pre(False);assert 'resource limits differ' in error
        results.append({'case':'real resource drift','result':'rejected','reason':error})
    finally:m.ex('docker','update','--memory','64m','--memory-swap','64m',edge)
    assert pre()['generation']==initial['generation']
    network='release-drift-only'
    m.ex('docker','network','create',network);m.ex('docker','network','connect',network,edge)
    try:
        error=pre(False);assert 'network membership differs' in error
        results.append({'case':'real extra network','result':'rejected','reason':error})
    finally:
        m.ex('docker','network','disconnect',network,edge);m.ex('docker','network','rm',network)
    original=m.ex('cat',b.state()['release']+'/nginx.conf')
    try:
        m.ex('sh','-c','cat > '+b.state()['release']+'/nginx.conf',input=original+b'\n# drift fixture\n')
        error=pre(False);assert 'configuration drift' in error
        results.append({'case':'actual release config change','result':'rejected','reason':error})
    finally:m.ex('sh','-c','cat > '+b.state()['release']+'/nginx.conf',input=original)
    assert pre()['healthy'] and b.ledger()==baseline
    b.save('runtime-drift',{'cases':results,'restored_healthy':True,'ledger_unchanged':True,'helper_files':initial['helper_files']})
    print('runtime resource/network/file drift PASS',flush=True)
def repeat():
    before=b.ledger();st=b.state();services=pre()['services'];result=b.deploy('repeat-with-existing-ledger')
    assert result['result']=='already_applied' and b.ledger()==before and b.state()==st
    assert {n:v['container_id'] for n,v in services.items()}=={n:v['container_id'] for n,v in result['services'].items()}
    b.save('idempotent-data-proof',{'ledger_before':before,'ledger_after':b.ledger(),'state_unchanged':True,'containers_unchanged':True,'result':result})
    print('repeat with existing wallet/logs PASS',flush=True)
def close():
    private=json.loads((R/'private-business.json').read_text());base='http://127.0.0.1:8080'
    private['root_token']=b.api(base,'POST','/api/user/login',{'username':'sixroot','password':(R/'admin-password').read_text()})['access_token']
    (R/'private-business.json').write_text(json.dumps(private))
    b.api(base,'PUT','/api/token/?status_only=true',{'id':private['token_id'],'status':2},private['root_token'])
    assert b.pg('SELECT count(*) FROM channels WHERE type=59 AND status=1')=='0'
    assert b.pg('SELECT status FROM tokens WHERE id='+str(private['token_id']))=='2'
    b.save('final-closure',{'active_pool_channels':0,'relay_token_disabled':True,'ledger':b.ledger(),'resource':b.sample(),'preflight':pre()})
    print('final keys closed, ledger and healthy proof saved',flush=True)
def sse():
    private=json.loads((R/'private-business.json').read_text());base='http://127.0.0.1:8080'
    cid=int(b.pg("SELECT id FROM channels WHERE name='six-sse'"))
    b.api(base,'POST','/api/channel/'+str(cid)+'/status',{'status':1},private['root_token'])
    try:
        start=time.monotonic()
        raw=b.api(base,'POST','/v1/chat/completions',{'model':'six-sse','messages':[{'role':'user','content':'post restore local'}],'max_tokens':16,'stream':True,'stream_options':{'include_usage':True}},private['relay_key'],raw=True)
        assert b'[DONE]' in raw
    finally:b.api(base,'POST','/api/channel/'+str(cid)+'/status',{'status':2},private['root_token'])
    deadline=time.monotonic()+30
    while b.ledger()['logs']!=11 and time.monotonic()<deadline:time.sleep(1)
    facts=b.ledger();assert facts['quota']==999846 and facts['logs']==facts['unique_logs']==11 and facts['token_used']==facts['applied']==facts['log_quota']==154 and facts['pending_outbox']==0,facts
    b.save('post-restore-sse',{'complete':True,'driver_seconds':time.monotonic()-start,'ledger':facts})
    print('post restore SSE and fresh unique ledger154 PASS',flush=True)
def official():
    before=b.ledger();archive=pathlib.Path('D:/AI/API/runtime/six-service-helper-20261031/release-v2.tar')
    b.approve(archive);result=b.deploy('final-official-package')
    assert result['bundle_sha256']=='ed0c3dba06d435852892483ca3647f635d3308f35536673bc62c1a2600a1f01d' and b.ledger()==before
    repeat()
def stop():
    m.ex('logrotate','--debug','/etc/logrotate.d/clearapi-six-stack')
    m.ex('logrotate','--force','/etc/logrotate.d/clearapi-six-stack')
    proof=json.loads(b.remote('stack-backup --keep-fenced'))
    b.dc('stop','-t','30')
    assert m.ex('docker','ps','-q').decode().strip()==''
    b.save('clean-stop',{'all_six_stopped':True,'new_encrypted_backup':proof,'logrotate_debug_force':'PASS','data_and_volumes_preserved':True})
    print('six services cleanly stopped and final encrypted backup retained',flush=True)
if __name__=='__main__':
    import sys
    for action in sys.argv[1:]:{'drift':drift,'repeat':repeat,'close':close,'sse':sse,'official':official,'stop':stop}[action]()
