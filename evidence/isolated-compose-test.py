"""Real substrate test only; not the GHCR-bound production adapter E2E."""
import hashlib
import json
from pathlib import Path
import subprocess
import time
from urllib.request import urlopen

ROOT=Path('/test-deploy')
ROOT.mkdir(exist_ok=True)
BASE='clearapi-local-isolated:a032815-20261023'
GOOD='clearapi-local-isolated:test-update'
BAD='clearapi-local-isolated:test-unhealthy'
report={'scope':'real Compose/HTTP/PostgreSQL/rollback substrate; production helper apply NOT executed',
        'executor_caddy':'sleep-only stand-ins; only restart preservation is tested',
        'steps':[]}

def run(*args):
    p=subprocess.run(args,capture_output=True,text=True)
    if p.returncode:
        raise RuntimeError('command failed: '+repr(args)+'\n'+p.stderr[-4000:])
    return p.stdout.strip()

def compose(*args):
    return run('docker','compose','--project-directory',str(ROOT),'-f',str(ROOT/'compose.json'),*args)

def inspect(service):
    cid=compose('ps','-a','-q',service)
    obj=json.loads(run('docker','inspect',cid))[0]
    return {'id':obj['Id'],'image':obj['Image'],'started':obj['State']['StartedAt']}

def wait_health():
    deadline=time.monotonic()+90
    while time.monotonic()<deadline:
        try:
            with urlopen('http://127.0.0.1:3000/api/status',timeout=2) as response:
                if response.status==200 and json.load(response).get('success') is True:
                    return
        except (OSError,ValueError):
            pass
        time.sleep(1)
    raise RuntimeError('Gateway health did not pass: '+compose('logs','--tail','35','new-api'))

def sql(query):
    return compose('exec','-T','postgres','psql','-U','clearapitest','-d','clearapitest','-Atc',query)

def database_snapshot():
    schema=compose('exec','-T','postgres','pg_dump','-U','clearapitest','-d','clearapitest','--schema-only')
    stable='\n'.join(line for line in schema.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))
    return {'schema_sha256':hashlib.sha256(stable.encode()).hexdigest(),
            'marker':sql('SELECT value FROM local_deploy_probe')}

def write_config(image):
    config['services']['new-api']['image']=image
    (ROOT/'compose.json').write_text(json.dumps(config,indent=2)+'\n')

# Fail closed unless this is the specifically owned disposable nested daemon.
assert Path('/original/scripts/local-deploy-vps.py').exists()
assert run('docker','info','--format','{{.ID}}')=='e3799c1f-9437-4e96-969e-c640d8a5f409'
assert json.loads(run('docker','ps','-aq','--format','{{json .}}') or 'null') is None

for tag,extra in ((GOOD,'LABEL clearapi.test.variant="frontend-update"\n'),
                  (BAD,'ENTRYPOINT ["/bin/false"]\n')):
    dockerfile=ROOT/(tag.rsplit(':',1)[1]+'.Dockerfile')
    dockerfile.write_text('FROM '+BASE+'\n'+extra)
    run('docker','build','--network=none','--pull=false','-f',str(dockerfile),'-t',tag,str(ROOT))

(ROOT/'business-config.json').write_text('{"registration":true,"billing":"unchanged-test-marker"}\n')
business_hash=hashlib.sha256((ROOT/'business-config.json').read_bytes()).hexdigest()
config={'name':'clearapi-local-deploy-isolated','services':{
    'postgres':{'image':'postgres:17.6-alpine','pull_policy':'never',
        'environment':{'POSTGRES_USER':'clearapitest','POSTGRES_PASSWORD':'isolated-test-only','POSTGRES_DB':'clearapitest'},
        'volumes':['pgdata:/var/lib/postgresql/data'],
        'healthcheck':{'test':['CMD-SHELL','pg_isready -U clearapitest'],'interval':'1s','timeout':'3s','retries':60}},
    'executor':{'image':'alpine:3.20','pull_policy':'never','command':['sleep','infinity']},
    'caddy':{'image':'alpine:3.20','pull_policy':'never','command':['sleep','infinity']},
    'new-api':{'image':BASE,'pull_policy':'never','depends_on':{'postgres':{'condition':'service_healthy'}},
        'ports':['127.0.0.1:3000:3000'],'stop_grace_period':'150s',
        'environment':{'SQL_DSN':'postgres://clearapitest:isolated-test-only@postgres:5432/clearapitest?sslmode=disable',
            'SESSION_SECRET':'isolated-test-session-secret-0000','CRYPTO_SECRET':'isolated-test-crypto-secret-00000',
            'GIN_MODE':'debug','CLEARAPI_POOL_ENABLED':'false','CLEARAPI_POOL_CLIPROXY_ENABLED':'false'},
        'volumes':['./business-config.json:/test/business-config.json:ro','gatewaydata:/data']}
},'volumes':{'pgdata':{},'gatewaydata':{}}}
write_config(BASE)
compose('up','-d','--pull','never')
wait_health()
shared={key:inspect(key) for key in ('postgres','executor','caddy')}
sql("CREATE TABLE local_deploy_probe (value text NOT NULL); INSERT INTO local_deploy_probe VALUES ('preserve-me')")
db=database_snapshot()
report['steps'].append('baseline Gateway HTTP and real PostgreSQL PASS')
previous_config=(ROOT/'compose.json').read_bytes()
write_config(GOOD)
compose('up','-d','--no-deps','--pull','never','new-api')
wait_health()
updated=inspect('new-api')
assert updated['image']==run('docker','image','inspect','--format','{{.Id}}',GOOD)
assert updated['image']!=run('docker','image','inspect','--format','{{.Id}}',BASE)
assert shared=={key:inspect(key) for key in shared}
assert db==database_snapshot()
report['steps'].append('real distinct image update; shared container IDs/start times and database schema/row unchanged PASS')

qualified_config=(ROOT/'compose.json').read_bytes()
write_config(BAD)
compose('up','-d','--no-deps','--pull','never','new-api')
time.sleep(2)
state=run('docker','inspect','--format','{{.State.Status}}',compose('ps','-a','-q','new-api'))
assert state=='exited'
try:
    with urlopen('http://127.0.0.1:3000/api/status',timeout=2):
        raise AssertionError('fault candidate unexpectedly healthy')
except OSError:
    pass
report['steps'].append('injected unhealthy candidate exited; HTTP health failure observed PASS')

(ROOT/'compose.json').write_bytes(qualified_config)
compose('up','-d','--no-deps','--pull','never','new-api')
wait_health()
assert inspect('new-api')['image']==updated['image']
assert (ROOT/'compose.json').read_bytes()==qualified_config
assert shared=={key:inspect(key) for key in shared}
assert db==database_snapshot()
assert hashlib.sha256((ROOT/'business-config.json').read_bytes()).hexdigest()==business_hash
report['steps'].append('previous qualified image restored healthy; config bytes, database schema/row, shared services preserved PASS')
report.update(result='PASS',database=db,shared_services=shared,
              restored_image_id=updated['image'],business_config_sha256=business_hash,
              limitation='manual test-harness rollback of same-backend fixture, not production helper automatic rollback or real login/billing/TLS acceptance')
(ROOT/'result.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
