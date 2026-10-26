import importlib.util
from pathlib import Path
import tempfile

spec=importlib.util.spec_from_file_location('adapter','/original/scripts/local-deploy-vps.py')
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
old=m.REGISTRY+'@sha256:'+'a'*64
new=m.REGISTRY+'@sha256:'+'b'*64
with tempfile.TemporaryDirectory(prefix='clearapi-compose-scope-') as directory:
    root=Path(directory)
    config={'app_dir':directory,'compose_file':'docker-compose.prod.yml',
            'service':'new-api','runtime_identity_dir':'/opt/clearapi/runtime/new-api'}
    (root/config['compose_file']).write_text('services:\n  new-api:\n    image: '+old+'\n  postgres:\n    image: postgres:17.6-alpine\n')
    override=root/'compose-candidate.yaml'
    content=m.override_yaml(config,old)
    override.write_bytes(content)
    override.chmod(0o600)
    m.verify_override_scope(config,new)
    content+=b'    environment:\n      PRESERVE_TEST_BUSINESS_SETTING: "true"\n'
    override.write_bytes(content)
    try:
        m.verify_override_scope(config,new)
    except m.DeployError as error:
        assert 'non-image configuration' in str(error)
    else:
        raise AssertionError('extra business configuration was silently discarded')
    assert override.read_bytes()==content
    assert not list(root.glob('.local-deploy-preview-*'))
print('PASS: real Compose preview permits image-only change, rejects dropped override environment, preserves bytes and cleans preview files')
