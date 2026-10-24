#!/usr/bin/env python3
"""ClearAPI local-built / SSH archive-transport deployment adapter.

Uses a checksummed Docker archive over SSH; GitHub is not an image transport.
Install only after explicitly authorized cutover. Never run on broken VPS.
"""
import argparse
from contextlib import contextmanager
import datetime as dt
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tarfile
import platform
import re
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit

sys.dont_write_bytecode = True

ROOT = Path('/opt/clearapi')
CONFIG = ROOT / 'config/local-deploy.json'
ACTIVATED = ROOT / 'config/local-deploy.enabled'
TEST_CONFIG = None
LOCK = Path('/run/lock/clearapi-production-deploy.lock')
COMMIT = re.compile(r'^[0-9a-f]{40}$')
SHA = re.compile(r'^sha256:[0-9a-f]{64}$')
HEX = re.compile(r'^[0-9a-f]{64}$')
IMAGE_REF = re.compile(r'^(?:clearapi-local:[a-f0-9]{40}|ghcr.io/a3225865526-wq/clearapi@sha256:[a-f0-9]{64})$')

_spec = importlib.util.spec_from_file_location('local_state', Path(__file__).with_name('local-deploy-state.py'))
STATE = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(STATE)

class DeployError(RuntimeError):
    pass


def secure_file(path, mode=None):
    s = path.lstat()
    if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
            or s.st_nlink != 1 or (s.st_mode & 0o007)
            or (mode is not None and stat.S_IMODE(s.st_mode) != mode)):
        raise DeployError('unsafe root-owned file: '+str(path))


def run(*args, input_data=None, env=None):
    if env is None and args[:2] == ('docker', 'compose') and '--env-file' in args:
        env = STATE.compose_environment()
    p=subprocess.run(list(args), input=input_data, capture_output=True, text=True, env=env)
    if p.returncode:
        raise DeployError('command failed: '+args[0]+' (exit='+str(p.returncode)+')')
    return p.stdout.strip()


def config():
    path = Path(TEST_CONFIG) if TEST_CONFIG else CONFIG
    secure_file(path, 0o600)
    if TEST_CONFIG:
        data=json.loads(path.read_text(encoding='utf-8'))
        if data.get('mode') != 'isolated-test-v1' or data.get('docker_daemon_id') != run('docker','info','--format','{{.ID}}'):
            raise DeployError('isolated test config identity mismatch')
        required={'app_dir','compose_file','service','runtime_identity_dir','local_url','public_url',
                  'backup_dir','docker_daemon_id','isolated','mode','test_backup_fail','db_user','db_name'}
        if (set(data) != required or data.get('isolated') is not True
                or data['service'] != 'new-api' or data['compose_file'] != 'docker-compose.prod.yml'
                or not data['app_dir'].startswith('/opt/clearapi-test/')
                or not data['runtime_identity_dir'].startswith('/opt/clearapi-test/')
                or data['runtime_identity_dir'] == '/opt/clearapi/runtime/new-api'
                or not data['db_user'] or not data['db_name']
                or not data['backup_dir'].startswith('/opt/clearapi-test/')
                or not data['local_url'].startswith('http://127.0.0.1:')
                or data['public_url'] != data['local_url']):
            raise DeployError('unsafe isolated test config')
        for key in ('app_dir', 'runtime_identity_dir', 'backup_dir'):
            resolved = Path(data[key]).resolve()
            if not resolved.is_relative_to('/opt/clearapi-test') or resolved == Path('/opt/clearapi-test'):
                raise DeployError('unsafe isolated test path')
        url=urlsplit(data['local_url'])
        if (url.scheme != 'http' or url.hostname != '127.0.0.1'
                or url.username is not None or url.password is not None
                or url.port is None or not 1 <= url.port <= 65535):
            raise DeployError('unsafe isolated test URL')
        return data
    secure_file(ACTIVATED, 0o600)
    data=json.loads(path.read_text(encoding='utf-8'))
    values={
        'app_dir':'/opt/clearapi/app', 'compose_file':'docker-compose.prod.yml',
        'service':'new-api', 'runtime_identity_dir':'/opt/clearapi/runtime/new-api',
        'local_url':'http://127.0.0.1:3000/api/status',
        'public_url':'https://www.clear999.com/api/status',
        'backup_service':'clearapi-backup.service',
        'backup_dir':'/opt/clearapi/backups',
        'archive_transport':True,
    }
    if data != values or ACTIVATED.read_text().strip() != 'LOCAL_DEPLOY_CUTOVER_APPROVED_V1':
        raise DeployError('unsupported layout or local cutover not activated')
    return data


def arch():
    value=platform.machine().lower()
    if value in ('x86_64','amd64'): return 'amd64'
    if value in ('aarch64','arm64'): return 'arm64'
    raise DeployError('unknown server architecture')


def compose_args(c):
    if not TEST_CONFIG and (Path(c['app_dir']) / STATE.NAME).exists():
        return STATE.compose_args(c['app_dir'])
    base=['docker','compose','--project-directory',c['app_dir'],'-f',str(Path(c['app_dir'])/c['compose_file'])]
    override=Path(c['app_dir'])/'compose-candidate.yaml'
    secure_file(override)
    return base+['-f',str(override)]


def image_id(ref):
    with tempfile.TemporaryDirectory(prefix='clearapi-image-') as folder:
        archive=Path(folder)/'image.tar'
        run('docker','save','--output',str(archive),ref)
        with tarfile.open(archive,'r') as bundle:
            entries=json.loads(bundle.extractfile('manifest.json').read())
            if len(entries)!=1: raise DeployError('image archive must contain one config')
            return 'sha256:'+hashlib.sha256(bundle.extractfile(entries[0]['Config']).read()).hexdigest()


def deployed(c):
    args=compose_args(c)
    cid=run(*(args+['ps','-q',c['service']]))
    if not re.fullmatch(r'[0-9a-f]{12,64}',cid):
        raise DeployError('Gateway not running; production recovery required first')
    state=json.loads(run('docker','inspect','--format','{{json .State}}',cid))
    if state.get('Running') is not True:
        raise DeployError('Gateway stopped')
    engine_id=run('docker','inspect','--format','{{.Image}}',cid)
    img=image_id(engine_id)
    effective=json.loads(run(*(args+['config','--format','json'])))['services'][c['service']]['image']
    if not IMAGE_REF.fullmatch(effective):
        raise DeployError('running Compose image reference is invalid')
    if image_id(effective)!=img:
        raise DeployError('Compose effective image does not match running image')
    rev=run('docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',engine_id)
    if not COMMIT.fullmatch(rev):
        raise DeployError('invalid running image revision')
    return {'container_id':cid,'image_id':img,'image_ref':effective,'commit':rev}

def verify_manifest(c,d):
    path=STATE.manifest_path(c['app_dir']) if not TEST_CONFIG else Path(c['app_dir'])/'release-manifest.json'
    secure_file(path)
    manifest=json.loads(path.read_text())
    current=manifest['releases']['current']
    if current.get('status') == 'adopted' and path.name == STATE.NAME:
        expected_status = 'adopted'
    else:
        expected_status = 'success'
    required={'status':expected_status,'candidate_commit':d['commit'], 'new_api_image_id':d['image_id'],
              'image_ref':d['image_ref']}
    if any(current.get(k)!=v for k,v in required.items()):
        raise DeployError('release manifest disagrees with running Gateway')
    return manifest


def probe(url):
    try:
        response=run('curl','-q','--noproxy','*','--silent','--show-error',
            '--proto','=http,https','--max-time','8','--max-filesize','65536',
            '--header','X-ClearAPI-Probe: local-deploy-v1',
            '--write-out','\nCLEARAPI_HTTP:%{http_code}',url)
        body,separator,status=response.rpartition('\nCLEARAPI_HTTP:')
        if not separator or status!='200':
            raise DeployError('status endpoint HTTP '+status)
        if len(body.encode())>65536 or json.loads(body).get('success') is not True:
            raise DeployError('status endpoint not PASS')
    except Exception as e:
        raise DeployError('health probe failed ('+type(e).__name__+'): '+str(e)) from e


def verify_database(c):
    pg=run(*(compose_args(c)+['ps','-q','postgres']))
    if not pg or run('docker','inspect','--format',
        '{{if .State.Health}}{{.State.Health.Status}}{{else}}unknown{{end}}',pg)!='healthy':
        raise DeployError('PostgreSQL not healthy')


def wait_health(c):
    # Compose start/recreate returns before the Gateway begins accepting HTTP.
    # Bound readiness polling; retain the actual final probe failure for audit.
    deadline=time.monotonic()+30
    while True:
        try:
            verify_database(c)
            probe(c['local_url'])
            probe(c['public_url'])
            return
        except DeployError as ex:
            if time.monotonic() >= deadline:
                raise DeployError('health readiness timed out; '+str(ex)) from ex
            time.sleep(1)


def preflight(c):
    d=deployed(c)
    verify_manifest(c,d)
    verify_database(c)
    probe(c['local_url'])
    probe(c['public_url'])
    return {'arch':arch(),'service':c['service'],**d,'healthy':True}


def adoption_snapshot(app):
    """Observe this already-running recovery instance; no container operation."""
    app = Path(app)
    run('systemctl', 'is-active', '--quiet', 'docker.service')
    for name, digest in STATE.FROZEN.items():
        if hashlib.sha256(STATE.read_file(app/name)).hexdigest() != digest:
            raise DeployError('original incident changed')
    fmt = ('{"Id":{{json .Id}},"Image":{{json .Image}},"Ref":{{json .Config.Image}},'
           '"Running":{{json .State.Running}},"Restarting":{{json .State.Restarting}},'
           '"StartedAt":{{json .State.StartedAt}},"Mounts":{{json .Mounts}},'
           '"Files":{{index .Config.Labels "com.docker.compose.project.config_files" | json}},'
           '"Hash":{{index .Config.Labels "com.docker.compose.config-hash" | json}}}')
    gateway=json.loads(run('docker','inspect','--format',fmt,'clearapi-new-api-1'))
    revision='a032815cb6244a7ee5e812de82f8300c02554e99'
    image='sha256:ed6f93a91285abab0f8730c7c14a36b5e52bfaed521ce8c4e798d2815da286b1'
    ref='ghcr.io/a3225865526-wq/clearapi@'+image
    if (gateway['Id']!='0a9a93c02646b1885323f6d4255277c1253e06794205d061f3fdbfca2cc1b56c'
            or gateway['Image']!=image or gateway['Ref']!=ref
            or gateway['Running'] is not True or gateway['Restarting']):
        raise DeployError('not the reviewed running Gateway')
    mounts=[m for m in gateway['Mounts'] if m.get('Destination')=='/run/clearapi']
    if (len(mounts)!=1 or mounts[0].get('Source')!='/opt/clearapi/runtime/new-api'
            or mounts[0].get('RW') is not False or mounts[0].get('Type')!='bind'):
        raise DeployError('running Gateway runtime mount changed')
    files=[str(app/name) for name in STATE.BASE_FILES]
    if gateway['Files'].split(',') != files:
        raise DeployError('running Gateway Compose inputs changed')
    env_file=STATE.environment_file(app)
    environment=hashlib.sha256(STATE.read_file(env_file,private=True)).hexdigest()
    args=['docker','compose','--project-directory',str(app),'--env-file',str(env_file)]
    for name in files:args+=['-f',name]
    calculated=run(*(args+['config','--hash','new-api'])).split()
    if len(calculated)!=2 or calculated[1]!=gateway['Hash']:
        raise DeployError('effective Compose differs from running Gateway')
    effective=json.loads(run(*(args+['config','--format','json'])))
    if (effective['services']['new-api']['image']!=ref or
            str(effective['services']['new-api'].get('environment',{}).get('CLEARAPI_POOL_ENABLED')).lower()!='false'):
        raise DeployError('running Gateway image or deferred PG03 mode changed')
    observed_revision=run('docker','image','inspect','--format',
        '{{index .Config.Labels "org.opencontainers.image.revision"}}',image)
    if observed_revision!=revision:raise DeployError('running Gateway OCI revision changed')
    run('python3','-I','/opt/clearapi/bin/runtime-identity-refresh.py','--check',
        '--container',gateway['Id'],'--image-ref',ref,'--expected-revision',revision,
        '--directory','/opt/clearapi/runtime/new-api')
    probe('http://127.0.0.1:3000/api/status')
    shared={}
    expected={'clearapi-postgres-1':'99299db0bed7ea3afdd0d3319369a68294032d2dec16c4f2231b1048fef044eb',
              'clearapi-pool-executor-1':'1081a1b576ad95e521c1d390b3cfe4d4f9c0eba880779a0391a7e9aef673e57f'}
    for name,cid in expected.items():
        info=json.loads(run('docker','inspect','--format',
            '{"Id":{{json .Id}},"Image":{{json .Image}},"Running":{{json .State.Running}},'
            '"Health":{{with index .State "Health"}}{{json .Status}}{{else}}null{{end}}}',name))
        if info['Id']!=cid or info['Running'] is not True or info['Health']!='healthy':
            raise DeployError('shared service changed or unhealthy')
        shared[name]={'container_id':cid,'image_id':info['Image']}
    return {'gateway':{'container_id':gateway['Id'],'image_ref':ref,'image_id':image,
                       'commit':revision,'started_at':gateway['StartedAt']},
            'shared':shared,'compose_hash':gateway['Hash'],
            'compose_sha256':{n:hashlib.sha256(STATE.read_file(app/n)).hexdigest() for n in STATE.BASE_FILES},
            'environment_sha256':environment}


@contextmanager
def deploy_lock():
    with LOCK.open('a+b') as lock:
        try:fcntl.flock(lock.fileno(),fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError:raise DeployError('another production deploy holds the lock')
        yield


def adopt_running(app, source_commit, execute=False):
    if not COMMIT.fullmatch(source_commit):raise DeployError('exact adoption source commit required')
    app=Path(app)
    with deploy_lock():
        if (app/STATE.NAME).exists() or (app/STATE.NAME).is_symlink():
            raise DeployError('instance already adopted; use ordinary preflight')
        before=adoption_snapshot(app)
        # Reuse the original edge identity/configuration check, without opening it.
        spec=importlib.util.spec_from_file_location('failed_recovery',Path(__file__).with_name('pg03-failed-recovery.py'))
        recovery=importlib.util.module_from_spec(spec);spec.loader.exec_module(recovery)
        original=recovery.WebsiteRestoration(recovery=recovery.Recovery(app,lambda args:run(*args)))
        original.edge(False)
        hold=recovery.TRAFFIC.TrafficHold(app,lambda args:run(*args))
        hold.verify(hold.binding(recovery.FAILED_RUN,recovery.DIGEST,recovery.COMMIT))
        if not execute:return {'mode':'read_only','result':'adoption_ready','snapshot':before}
        config_id=image_id(before['gateway']['image_ref'])
        overlay=app/'compose-local.yaml'
        if overlay.exists() or overlay.is_symlink():raise DeployError('local Compose overlay already exists')
        # No Compose up/start: this only creates a future image-only terminal layer.
        STATE.publish(overlay,{'services':{'new-api':{'image':before['gateway']['image_ref']}}})
        try:
            args=['docker','compose','--project-directory',str(app),'--env-file',str(STATE.environment_file(app))]
            for n in STATE.BASE_FILES:args+=['-f',str(app/n)]
            baseline=json.loads(run(*(args+['config','--format','json'])))
            proposed=json.loads(run(*(args+['-f',str(overlay),'config','--format','json'])))
            if baseline!=proposed or adoption_snapshot(app)!=before:
                raise DeployError('adoption would change live configuration')
            original.edge(False)
            g=before['gateway']
            state={'schema':'clearapi.local-deploy-state/v1',
                   'adoption':{**before,'original_incident':dict(STATE.FROZEN),
                       'source_commit':source_commit,'recorded_at':dt.datetime.now(dt.timezone.utc).isoformat()},
                   'releases':{'current':{'status':'adopted','deployment_method':'existing-running-adoption-v1',
                       'candidate_commit':g['commit'],'new_api_image_id':config_id,
                       'docker_engine_image_id':g['image_id'],
                       'image_ref':g['image_ref'],'image_digest':g['image_ref'].split('@')[1]},'previous':{}}}
            STATE.publish(app/STATE.NAME,state)
        except BaseException:
            # An unpublished adoption leaves the old contract authoritative.
            if not (app/STATE.NAME).exists():overlay.unlink()
            raise
        return {'result':'adopted_running_instance','gateway_replaced':False,'snapshot':before}


def atomic_write(path,content,mode=0o600):
    fd,name=tempfile.mkstemp(prefix='.local-deploy-',dir=str(path.parent))
    try:
        os.fchmod(fd,mode)
        with os.fdopen(fd,'wb') as f:
            f.write(content)
            f.flush()
            os.fsync(f.fileno())
        os.replace(name,path)
    finally:
        if os.path.exists(name): os.unlink(name)


def backup(c):
    if TEST_CONFIG:
        if c['test_backup_fail']:
            raise DeployError('isolated test backup failure requested')
        directory=Path(c['backup_dir'])/'manifests'
        directory.mkdir(parents=True,exist_ok=True,mode=0o700)
        archive_dir=Path(c['backup_dir'])/'archives'
        archive_dir.mkdir(parents=True,exist_ok=True,mode=0o700)
        stamp=dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
        args=compose_args(c)+['exec','-T','postgres','pg_dump','-U',c['db_user'],'-d',c['db_name'],'-Fc']
        result=subprocess.run(args,capture_output=True)
        if result.returncode or not result.stdout:
            raise DeployError('isolated PostgreSQL backup failed')
        artifact=archive_dir/(stamp+'.dump')
        artifact.write_bytes(result.stdout)
        os.chmod(artifact,0o600)
        digest=hashlib.sha256(result.stdout).hexdigest()
        proof={'mode':'isolated-test-v1','backup_id':stamp,'docker_daemon_id':c['docker_daemon_id'],
               'bytes':len(result.stdout),'sha256':digest,'database':c['db_name'],'result':'success'}
        record=directory/(stamp+'.json')
        record.write_text(json.dumps(proof,sort_keys=True)+'\n')
        os.chmod(record,0o600)
        if hashlib.sha256(artifact.read_bytes()).hexdigest()!=digest:
            raise DeployError('isolated backup proof mismatch')
        return stamp
    directory=Path(c['backup_dir'])/'manifests'
    before=set(x.name for x in directory.glob('*.json'))
    run('systemctl','start',c['backup_service'])
    if run('systemctl','show','-p','Result','--value',c['backup_service'])!='success':
        raise DeployError('backup systemd unit failed')
    fresh=[x for x in directory.glob('*.json') if x.name not in before]
    if len(fresh)!=1:
        raise DeployError('no unique fresh backup proof')
    record=fresh[0]
    secure_file(record)
    obj=json.loads(record.read_text())
    bid=obj.get('backup_id','')
    encmeta=obj.get('encryption',{}).get('encrypted_artifact',{})
    enc=Path(c['backup_dir'])/'encrypted'/bid/encmeta.get('name','')
    if (obj.get('backup_schema_version')!=2 or obj.get('backup_identity_schema_version')!=1
            or not isinstance(obj.get('running_identity'),dict)
            or not all(obj['running_identity'].get(x) for x in ('oci_revision','docker_image_id','container_id'))
            or not re.fullmatch(r'\d{8}T\d{6}Z',bid) or record.name!=bid+'.json'
            or not HEX.fullmatch(str(encmeta.get('sha256',''))) or not enc.is_file()
            or enc.stat().st_size!=encmeta.get('bytes')):
        raise DeployError('backup artifact/identity invalid')
    h=hashlib.sha256()
    with enc.open('rb') as f:
        for chunk in iter(lambda:f.read(1024*1024),b''): h.update(chunk)
    if h.hexdigest()!=encmeta['sha256']:
        raise DeployError('backup ciphertext checksum mismatch')
    live=deployed(c)
    identity=obj['running_identity']
    if any(identity.get(key)!=live[value] for key,value in
           (('container_id','container_id'),('oci_revision','commit'),('docker_image_id','image_id'))):
        raise DeployError('fresh backup does not identify the current running Gateway')
    state_path=STATE.manifest_path(c['app_dir'])
    if identity.get('release_manifest_sha256')!=hashlib.sha256(state_path.read_bytes()).hexdigest():
        raise DeployError('backup deployment metadata changed')
    return bid


def identity_prepare(c):
    run('python3','-I','/opt/clearapi/bin/runtime-identity-refresh.py','--prepare-only',
        '--directory',c['runtime_identity_dir'])


def identity_refresh(c,commit,ref):
    cid=run(*(compose_args(c)+['ps','-q',c['service']]))
    run('python3','-I','/opt/clearapi/bin/runtime-identity-refresh.py','--reconcile',
        '--container',cid,'--image-ref',ref,'--expected-revision',commit,
        '--directory',c['runtime_identity_dir'])


def override_yaml(c,ref):
    if not IMAGE_REF.fullmatch(ref):
        raise DeployError('invalid fixed local image ref')
    return (f'''services:
  {c["service"]}:
    image: {ref}
    volumes:
      - type: bind
        source: {c["runtime_identity_dir"]}
        target: /run/clearapi
        read_only: true
        bind:
          create_host_path: false
''').encode()


def receive_archive(c, archive_sha256, archive_bytes, commit, expected_id):
    if not HEX.fullmatch(archive_sha256) or archive_bytes <= 0:
        raise DeployError('invalid archive identity')
    folder = Path(c['app_dir']) / '.local-deploy-incoming'
    folder.mkdir(mode=0o700, exist_ok=True)
    archive = folder / (archive_sha256 + '.tar')
    with archive.open('wb') as out:
        while True:
            chunk = sys.stdin.buffer.read(1024 * 1024)
            if not chunk: break
            out.write(chunk)
    if archive.stat().st_size != archive_bytes:
        raise DeployError('archive byte count mismatch')
    h = hashlib.sha256()
    with archive.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''): h.update(chunk)
    if h.hexdigest() != archive_sha256:
        raise DeployError('archive checksum mismatch')
    with tarfile.open(archive, 'r') as bundle:
        entries=json.loads(bundle.extractfile('manifest.json').read())
        if len(entries)!=1 or not re.fullmatch(r'(?:[a-f0-9]{64}\.json|blobs/sha256/[a-f0-9]{64})', entries[0]['Config']):
            raise DeployError('invalid saved image manifest')
        config_name=entries[0]['Config']
        config_bytes=bundle.extractfile(config_name).read()
        if 'sha256:'+hashlib.sha256(config_bytes).hexdigest() != expected_id:
            raise DeployError('archive config ID mismatches local build')
    run('docker','load','--input',str(archive))
    ref = 'clearapi-local:' + commit
    if image_id(ref)!=expected_id:
        raise DeployError('loaded image config ID mismatches local build')
    revision=run('docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',ref)
    architecture=run('docker','image','inspect','--format','{{.Architecture}}',ref)
    if revision != commit or architecture != arch():
        raise DeployError('loaded image revision or architecture mismatch')
    return ref

def manifest_next(previous, old, commit, ref, image, status, archive_sha256=None):
    m=json.loads(json.dumps(previous))
    current=m['releases']['current']
    new={'tag':ref,'candidate_commit':commit,'new_api_image_id':image,
         'image_ref':ref, 'archive_sha256':archive_sha256,
         'previous_image_id':old['image_id'], 'workflow_run':'local-ssh',
         'deployed_at_utc':dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
         'deployment_method':'local-build-ssh-archive-v1',
         'migration_mode':'preflight-only (no flags configured)', 'status':status}
    for k in ('postgres_image','migrations_applied'):
        if current.get(k):new[k]=current[k]
    m['releases']['previous']=current
    m['releases']['current']=new
    m['updated_at_utc']=new['deployed_at_utc']
    return (json.dumps(m,sort_keys=True,indent=2)+'\n').encode()


def verify_override_scope(c, ref):
    """Reject replacement overrides that would discard existing configuration."""
    expected=json.loads(run(*(compose_args(c)+['config','--format','json'])))
    expected['services'][c['service']]['image']=ref
    fd,name=tempfile.mkstemp(prefix='.local-deploy-preview-',suffix='.json',dir=c['app_dir'])
    try:
        with os.fdopen(fd,'wb') as stream:
            stream.write(override_yaml(c,ref))
        args = (STATE.compose_args(c['app_dir'], name) if not TEST_CONFIG and
                (Path(c['app_dir']) / STATE.NAME).exists() else
                ['docker','compose','--project-directory',c['app_dir'],
                 '-f',str(Path(c['app_dir'])/c['compose_file']),'-f',name])
        candidate=json.loads(run(*(args+['config','--format','json'])))
        if candidate!=expected:
            raise DeployError('candidate override would change non-image configuration; preserve existing configuration before cutover')
    finally:
        os.unlink(name)


def apply(c,a,archive_sha256=None,archive_bytes=None):
    if not COMMIT.fullmatch(a.commit) or not SHA.fullmatch(a.image_id) or not SHA.fullmatch(a.previous_image_id):
        raise DeployError('invalid candidate image identity')
    ref='clearapi-local:'+a.commit
    with deploy_lock():
        old=preflight(c)
        if old['image_id']!=a.previous_image_id or old['commit']==a.commit:
            raise DeployError('predecessor changed or candidate already deployed')
        verify_override_scope(c,ref)
        if archive_sha256 is None: raise DeployError('archive transport required')
        receive_archive(c,archive_sha256,archive_bytes,a.commit,a.image_id)
        bid=backup(c) if a.scope=='backend' else None
        manifest=STATE.manifest_path(c['app_dir']) if not TEST_CONFIG else Path(c['app_dir'])/'release-manifest.json'
        override=Path(c['app_dir'])/('compose-local.yaml' if manifest.name == STATE.NAME else 'compose-candidate.yaml')
        original_override=override.read_bytes()
        original_manifest=manifest.read_bytes()
        previous_obj=json.loads(original_manifest)
        replaced=False
        switch_attempted=False
        try:
            identity_prepare(c)
            # Keep backup-identity contract: current manifest must match running
            # candidate only after service replacement. Interim pending is
            # protected by the production deploy lock.
            atomic_write(override,override_yaml(c,ref))
            replaced=True  # override itself is already a production-side state change
            atomic_write(manifest,manifest_next(previous_obj,old,a.commit,ref,a.image_id,'pending',archive_sha256))
            switch_attempted=True
            run(*(compose_args(c)+['up','-d','--no-deps','--timeout','150',c['service']]))
            identity_refresh(c,a.commit,ref)
            deployed_now=deployed(c)
            if deployed_now['image_id']!=a.image_id or deployed_now['commit']!=a.commit:
                raise DeployError('new container image identity mismatch')
            wait_health(c)
            success=manifest_next(previous_obj,old,a.commit,ref,a.image_id,'success',archive_sha256)
            atomic_write(manifest,success)
            verify_manifest(c,deployed_now)
            print(json.dumps({'result':'success','commit':a.commit,'archive_sha256':archive_sha256,
                              'image_id':a.image_id,'service':c['service'],
                              'backup_id':bid,'health':'PASS','rollback':False},sort_keys=True))
        except Exception as ex:
            if not replaced:raise
            if not switch_attempted:
                # No container replacement attempt: restore configuration
                # atomically even for backend changes; migration was not run.
                atomic_write(override,original_override)
                atomic_write(manifest,original_manifest)
                raise DeployError('candidate failed before container switch; old configuration restored') from ex
            if a.scope=='backend':
                # DB may have been modified by the new image, even without
                # explicit migration flags; never silently restore/redeploy.
                raise DeployError('backend candidate failed after switch; migration state UNKNOWN; MANUAL RECOVERY required; cause: '+str(ex)) from ex
            try:
                atomic_write(override,original_override)
                atomic_write(manifest,original_manifest)
                run(*(compose_args(c)+['up','-d','--no-deps','--timeout','150',c['service']]))
                identity_refresh(c,old['commit'],old['image_ref'])
                restored=deployed(c)
                if restored['image_id']!=old['image_id']:
                    raise DeployError('rollback did not restore original image')
                verify_manifest(c,restored)
                wait_health(c)
                raise DeployError('frontend candidate FAILED; previous exact image restored and healthy; cause: '+str(ex)) from ex
            except DeployError as rollback_error:
                if str(rollback_error).startswith('frontend candidate FAILED; previous exact image restored'):
                    raise
                raise DeployError('ROLLBACK FAILED; manual incident recovery required; cause: '+str(rollback_error)) from rollback_error


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--config-path', default='')
    sub=parser.add_subparsers(dest='mode',required=True)
    sub.add_parser('preflight')
    for mode in ('adopt-check','adopt-running'):
        adoption=sub.add_parser(mode)
        adoption.add_argument('--source-commit',required=True)
    ap=sub.add_parser('receive')
    for name in ('commit','image-id','previous-image-id'):
        ap.add_argument('--'+name,required=True)
    ap.add_argument('--archive-sha256',required=True)
    ap.add_argument('--archive-bytes',required=True,type=int)
    ap.add_argument('--scope',required=True,choices=('frontend','backend'))
    a=parser.parse_args()
    if os.geteuid()!=0:raise DeployError('root required; fixed sudoers entry only')
    global TEST_CONFIG
    TEST_CONFIG = a.config_path or None
    if a.mode in ('adopt-check','adopt-running'):
        if TEST_CONFIG:raise DeployError('test config cannot authorize production adoption')
        print(json.dumps(adopt_running(ROOT/'app',a.source_commit,a.mode=='adopt-running'),sort_keys=True))
        return
    c=config()
    if a.mode=='preflight': print(json.dumps(preflight(c),sort_keys=True))
    else:apply(c,a,a.archive_sha256,a.archive_bytes)

if __name__=='__main__':
    try:main()
    except (DeployError,OSError,ValueError,KeyError,TypeError) as ex:
        print('BLOCKED: '+str(ex),file=sys.stderr)
        sys.exit(2)
