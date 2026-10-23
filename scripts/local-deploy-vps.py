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
from urllib.request import urlopen
from urllib.parse import urlsplit

ROOT = Path('/opt/clearapi')
CONFIG = ROOT / 'config/local-deploy.json'
ACTIVATED = ROOT / 'config/local-deploy.enabled'
TEST_CONFIG = None
LOCK = Path('/run/lock/clearapi-production-deploy.lock')
COMMIT = re.compile(r'^[0-9a-f]{40}$')
SHA = re.compile(r'^sha256:[0-9a-f]{64}$')
HEX = re.compile(r'^[0-9a-f]{64}$')
IMAGE_REF = re.compile(r'^(?:clearapi-local:[a-f0-9]{40}|ghcr.io/a3225865526-wq/clearapi@sha256:[a-f0-9]{64})$')

class DeployError(RuntimeError):
    pass


def secure_file(path, mode=None):
    s = path.lstat()
    if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
            or s.st_nlink != 1 or (s.st_mode & 0o007)
            or (mode is not None and stat.S_IMODE(s.st_mode) != mode)):
        raise DeployError('unsafe root-owned file: '+str(path))


def run(*args, input_data=None, env=None):
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
    path=Path(c['app_dir'])/'release-manifest.json'
    secure_file(path)
    manifest=json.loads(path.read_text())
    current=manifest['releases']['current']
    required={'status':'success','candidate_commit':d['commit'], 'new_api_image_id':d['image_id'],
              'image_ref':d['image_ref']}
    if any(current.get(k)!=v for k,v in required.items()):
        raise DeployError('release manifest disagrees with running Gateway')
    return manifest


def probe(url):
    try:
        with urlopen(url,timeout=8) as response:
            if response.status!=200 or json.loads(response.read(65536)).get('success') is not True:
                raise DeployError('status endpoint not PASS')
    except Exception as e:
        raise DeployError('health probe failed ('+type(e).__name__+')') from e


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
        candidate=json.loads(run('docker','compose','--project-directory',c['app_dir'],
            '-f',str(Path(c['app_dir'])/c['compose_file']),'-f',name,
            'config','--format','json'))
        if candidate!=expected:
            raise DeployError('candidate override would change non-image configuration; preserve existing configuration before cutover')
    finally:
        os.unlink(name)


def apply(c,a,archive_sha256=None,archive_bytes=None):
    if not COMMIT.fullmatch(a.commit) or not SHA.fullmatch(a.image_id) or not SHA.fullmatch(a.previous_image_id):
        raise DeployError('invalid candidate image identity')
    ref='clearapi-local:'+a.commit
    with LOCK.open('a+b') as lock:
        try: fcntl.flock(lock.fileno(),fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError:raise DeployError('another production deploy holds the lock')
        old=preflight(c)
        if old['image_id']!=a.previous_image_id or old['commit']==a.commit:
            raise DeployError('predecessor changed or candidate already deployed')
        verify_override_scope(c,ref)
        if archive_sha256 is None: raise DeployError('archive transport required')
        receive_archive(c,archive_sha256,archive_bytes,a.commit,a.image_id)
        bid=backup(c) if a.scope=='backend' else None
        override=Path(c['app_dir'])/'compose-candidate.yaml'
        manifest=Path(c['app_dir'])/'release-manifest.json'
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
    c=config()
    if a.mode=='preflight': print(json.dumps(preflight(c),sort_keys=True))
    else:apply(c,a,a.archive_sha256,a.archive_bytes)

if __name__=='__main__':
    try:main()
    except (DeployError,OSError,ValueError,KeyError,TypeError) as ex:
        print('BLOCKED: '+str(ex),file=sys.stderr)
        sys.exit(2)
