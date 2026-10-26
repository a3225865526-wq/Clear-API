#!/usr/bin/env python3
"""Root-only isolated reboot recovery regression; no production Docker or DB."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
SCRIPTS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('boot', SCRIPTS / 'runtime-boot-recovery.py')
BOOT = importlib.util.module_from_spec(spec)
spec.loader.exec_module(BOOT)
REVISION = 'a' * 40
DIGEST = 'sha256:' + 'b' * 64
IMAGE_ID = 'sha256:' + 'c' * 64
REF = 'ghcr.io/a3225865526-wq/clearapi@' + DIGEST


@unittest.skipUnless(hasattr(os, 'geteuid') and os.geteuid() == 0, 'Linux root required')
class BootRecoveryTest(unittest.TestCase):
    def test_inactive_docker_is_rejected_before_inspection_or_socket_activation(self):
        for mode in ('check', 'reconcile'):
            with self.subTest(mode=mode), patch.object(sys, 'argv', ['boot', mode]), \
                    patch.object(BOOT.subprocess, 'run', side_effect=subprocess.CalledProcessError(3, 'systemctl')) as command, \
                    patch.object(BOOT, 'load_helper') as helper, self.assertRaises(SystemExit):
                BOOT.main()
            helper.assert_not_called()
            command.assert_called_once_with(['systemctl', 'is-active', '--quiet', 'docker.service'],
                                             check=True, capture_output=True, timeout=5)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='clearapi-boot-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.app = self.root / 'app'
        self.app.mkdir(mode=0o750)
        self.directory = self.root / 'runtime'
        helper_path = self.root / 'runtime-identity-refresh.py'
        helper_path.write_bytes((SCRIPTS / 'runtime-identity-refresh.py').read_bytes())
        helper_path.chmod(0o644)
        self.helper = BOOT.load_helper(helper_path)
        self.current = dict(status='success', image_ref=REF, candidate_commit=REVISION,
                            image_digest=DIGEST, new_api_image_id=IMAGE_ID)
        self.write_manifest()
        self.running = True
        self.ref = REF
        self.container_id = 'd' * 64
        self.started_at = '2026-01-01T00:00:00Z'
        self.calls = []
        self.helper.docker_json = self.docker_json

    def write_manifest(self):
        (self.app / 'release-manifest.json').write_text(json.dumps({'releases': {'current': self.current}}))

    def docker_json(self, *args):
        self.calls.append(args)
        self.assertIn(args[0], ('inspect', 'image'))
        if args[0] == 'inspect':
            data = dict(Id=self.container_id, Image=IMAGE_ID, Ref=self.ref,
                        Running=self.running, Restarting=False,
                        State=dict(Running=self.running, Restarting=False, StartedAt=self.started_at),
                        Config=dict(Image=self.ref),
                        Mounts=[dict(Destination='/run/clearapi', Type='bind', RW=False,
                                     Source=str(self.directory))])
        else:
            digests = [] if self.ref.startswith('clearapi-local:') else [self.ref]
            data = dict(Id=IMAGE_ID, Digests=digests, RepoDigests=digests, Revision=REVISION,
                        Config={'Labels': {'org.opencontainers.image.revision': REVISION}})
        return [data]

    def recover(self):
        expected = BOOT.approved_identity(self.app)
        with patch.object(BOOT, 'approved_identity', return_value=expected):
            return BOOT.reconcile(self.helper, expected, self.directory, wait_seconds=0)

    def test_actual_tmpfiles_recreates_lost_directory_then_reconciles_new_process(self):
        config = self.root / 'tmpfiles.conf'
        config.write_text(BOOT.ASSETS['/etc/tmpfiles.d/clearapi-runtime.conf'].replace(
            str(BOOT.DIRECTORY), str(self.directory)))
        subprocess.run(['systemd-tmpfiles', '--create', str(config)], check=True, capture_output=True)
        self.assertEqual(self.recover(), 'repaired')
        path = self.directory / 'runtime-identity.json'
        old = json.loads(path.read_text())
        self.assertEqual(self.recover(), 'unchanged')
        shutil.rmtree(self.directory)  # Only the checked TemporaryDirectory fixture.
        self.assertEqual(BOOT.directory_state(self.directory), 'missing')
        self.container_id, self.started_at = 'e' * 64, '2026-02-01T00:00:00Z'
        subprocess.run(['systemd-tmpfiles', '--create', str(config)], check=True, capture_output=True)
        self.assertEqual(self.recover(), 'repaired')
        new = json.loads(path.read_text())
        self.assertNotEqual(old['container_id'], new['container_id'])
        self.assertEqual(new['started_at'], self.started_at)
        self.assertEqual(path.stat().st_mode & 0o777, 0o400)
        self.assertTrue(all(call[0] in ('inspect', 'image') for call in self.calls))

    def test_cold_boot_creates_missing_parent_before_tmpfiles_nested_directory(self):
        directory = self.root / 'runtime' / 'new-api'
        self.assertFalse(directory.parent.exists())
        config = self.root / 'tmpfiles.conf'
        content = BOOT.ASSETS['/etc/tmpfiles.d/clearapi-runtime.conf'].replace(
            str(BOOT.DIRECTORY), str(directory))
        config.write_text(content)
        BOOT.prepare_runtime_directory(directory, config)
        self.assertEqual(directory.stat().st_mode & 0o777, 0o750)
        self.assertEqual(directory.parent.stat().st_mode & 0o777, 0o755)

    def test_cold_boot_preserves_unsafe_existing_runtime_parent(self):
        directory = self.root / 'runtime' / 'new-api'
        directory.parent.mkdir(mode=0o755)
        directory.parent.chmod(0o777)
        config = self.root / 'tmpfiles.conf'
        config.write_text(BOOT.ASSETS['/etc/tmpfiles.d/clearapi-runtime.conf'].replace(
            str(BOOT.DIRECTORY), str(directory)))
        with self.assertRaisesRegex(BOOT.Blocked, 'unsafe runtime parent'):
            BOOT.prepare_runtime_directory(directory, config)
        self.assertFalse(directory.exists())

    def test_durable_runtime_mount_reconciles_after_process_restart_without_volatile_directory(self):
        self.directory.mkdir(mode=0o750)
        volatile = self.root / 'lost-run-directory'
        expected = BOOT.approved_identity(self.app)
        frozen = (self.app / 'release-manifest.json').read_bytes()
        with patch.object(BOOT, 'DIRECTORY', self.directory), \
                patch.object(BOOT, 'approved_identity', return_value=expected):
            mounted = BOOT.runtime_directory(self.helper, expected)
            self.assertEqual(mounted, self.directory)
            self.assertEqual(BOOT.reconcile(self.helper, expected, mounted, wait_seconds=0), 'repaired')
            self.container_id = 'e' * 64
            self.assertEqual(BOOT.reconcile(self.helper, expected, mounted, wait_seconds=0), 'repaired')
        self.assertEqual(json.loads((self.directory / 'runtime-identity.json').read_text())['container_id'], self.container_id)
        self.assertFalse(volatile.exists())
        self.assertEqual((self.app / 'release-manifest.json').read_bytes(), frozen)

    def test_boot_runtime_source_rejects_mount_drift_and_frozen_incident_switch(self):
        self.directory.mkdir(mode=0o750)
        expected = BOOT.approved_identity(self.app)
        baseline = self.docker_json('inspect')[0]
        cases = [dict(baseline, Image='sha256:' + 'f' * 64),
                 dict(baseline, Mounts=[]), dict(baseline, Mounts=baseline['Mounts'] * 2)]
        for change in ({'RW': True}, {'Type': 'volume'}, {'Source': '/tmp/foreign-runtime'}):
            cases.append(dict(baseline, Mounts=[dict(baseline['Mounts'][0], **change)]))
        with patch.object(BOOT, 'DIRECTORY', self.directory):
            for data in cases:
                with self.subTest(data=data), patch.object(self.helper, 'docker_json', return_value=[data]), \
                        self.assertRaises(BOOT.Blocked):
                    BOOT.runtime_directory(self.helper, expected)
            with self.assertRaisesRegex(BOOT.Blocked, 'frozen incident'):
                BOOT.runtime_directory(self.helper, (REF, BOOT.FAILED_COMMIT, IMAGE_ID))
        self.assertFalse((self.directory / 'runtime-identity.json').exists())

    def test_read_only_inspection_accepts_stopped_gateway_without_creating_directory(self):
        self.running = False
        self.assertEqual(BOOT.check_image(self.helper, BOOT.approved_identity(self.app), self.directory), 'stopped')
        self.assertFalse(self.directory.exists())

    def test_archive_boot_uses_config_digest_and_preserves_local_runtime_contract(self):
        self.ref = 'clearapi-local:' + REVISION
        config_id = 'sha256:' + 'f' * 64  # Containerd index differs from saved config.
        self.current = dict(status='success', image_ref=self.ref, candidate_commit=REVISION,
                            new_api_image_id=config_id, archive_sha256='1' * 64,
                            deployment_method='local-build-ssh-archive-v1')
        self.write_manifest()
        self.directory.mkdir(mode=0o750)
        expected = BOOT.approved_identity(self.app)
        frozen = (self.app / 'release-manifest.json').read_bytes()
        with patch.object(self.helper, 'archive_image_id', return_value=config_id), \
                patch.object(BOOT, 'DIRECTORY', self.directory):
            self.assertEqual(BOOT.runtime_directory(self.helper, expected), self.directory)
            self.assertEqual(BOOT.check_image(self.helper, expected, self.directory), 'running')
            self.assertEqual(self.recover(), 'repaired')
            self.assertEqual(self.recover(), 'unchanged')
        identity = json.loads((self.directory / 'runtime-identity.json').read_text())
        self.assertEqual(identity['docker_image_id'], IMAGE_ID)
        self.assertNotIn('image_digest', identity)
        self.assertEqual((self.app / 'release-manifest.json').read_bytes(), frozen)
        with patch.object(self.helper, 'archive_image_id', return_value=IMAGE_ID), \
                self.assertRaisesRegex(BOOT.Blocked, 'container image drift'):
            BOOT.check_image(self.helper, expected, self.directory)
        for change in ({'status': 'pending'}, {'status': 'failed'}, {'archive_sha256': ''},
                       {'deployment_method': 'isolated-test-v1'},
                       {'image_ref': 'clearapi-local:' + 'b' * 40}):
            with self.subTest(change=change):
                baseline = self.current.copy()
                self.current.update(change)
                self.write_manifest()
                with self.assertRaises(BOOT.Blocked):
                    BOOT.approved_identity(self.app)
                self.current = baseline

    def test_archive_runtime_rejects_tag_revision_mismatch(self):
        with self.assertRaisesRegex(self.helper.IdentityError, 'REVISION_MISMATCH'):
            self.helper.observe(BOOT.CONTAINER, 'clearapi-local:' + 'b' * 40,
                                REVISION, self.directory)

    def test_reboot_with_stopped_gateway_preserves_ingress_and_never_starts_it(self):
        hold = self.app / 'controlled-launch-state.json'
        hold.write_text(json.dumps({'stage': 'isolated', 'edge': {'running': True}}))
        frozen = {path.name: path.read_bytes() for path in self.app.iterdir()}
        config = self.root / 'tmpfiles.conf'
        config.write_text('d ' + str(self.directory) + ' 0750 root root -\n')
        subprocess.run(['systemd-tmpfiles', '--create', str(config)], check=True, capture_output=True)
        self.running = False
        with self.assertRaises(BOOT.Blocked):
            self.recover()
        self.assertFalse((self.directory / 'runtime-identity.json').exists())
        self.assertEqual({path.name: path.read_bytes() for path in self.app.iterdir()}, frozen)
        self.assertTrue(all(call[0] in ('inspect', 'image') for call in self.calls))

    def test_reviewed_boot_asset_install_is_idempotent_without_starting_services(self):
        script = self.root / 'runtime-boot-recovery.py'
        script.write_bytes((SCRIPTS / 'runtime-boot-recovery.py').read_bytes())
        assets = {str(self.root / name.lstrip('/')): content for name, content in BOOT.ASSETS.items()}
        for name in assets:
            Path(name).parent.mkdir(parents=True, exist_ok=True)
        with patch.object(BOOT, '__file__', str(script)), patch.object(BOOT, 'SCRIPT', str(script)), \
                patch.object(BOOT, 'ASSETS', assets), patch.object(BOOT, 'load_helper'), \
                patch.object(BOOT, 'approved_identity'), patch.object(BOOT, 'directory_state'), \
                patch.object(BOOT.subprocess, 'run') as command:
            BOOT.install()
            before = {name: (Path(name).read_bytes(), Path(name).stat().st_mtime_ns) for name in assets}
            BOOT.install()
            self.assertEqual(BOOT.check_assets(), 'ready')
        self.assertEqual({name: (Path(name).read_bytes(), Path(name).stat().st_mtime_ns) for name in assets}, before)
        expected = [['systemctl', 'daemon-reload'], ['systemctl', 'enable', 'clearapi-runtime-identity.service']]
        self.assertEqual([call.args[0] for call in command.call_args_list], expected * 2)
        self.assertFalse(self.directory.exists())

    def test_directory_symlink_owner_mode_and_identity_link_are_blocked(self):
        self.directory.symlink_to(self.app)
        with self.assertRaises(BOOT.Blocked):
            BOOT.directory_state(self.directory)
        self.directory.unlink()
        self.directory.mkdir(mode=0o777)
        with self.assertRaises(BOOT.Blocked):
            BOOT.directory_state(self.directory)
        self.directory.chmod(0o750)
        os.chown(self.directory, 1000, 0)
        with self.assertRaises(BOOT.Blocked):
            BOOT.directory_state(self.directory)
        os.chown(self.directory, 0, 0)
        (self.directory / 'runtime-identity.json').symlink_to(self.app / 'release-manifest.json')
        with self.assertRaises(self.helper.IdentityError):
            self.recover()

    def test_image_drift_and_pending_or_arbitrary_failed_authority_are_blocked(self):
        self.directory.mkdir(mode=0o750)
        original = self.helper.docker_json
        def changed(*args):
            data = copy.deepcopy(original(*args))
            if args[0] == 'inspect':
                data[0]['Image'] = 'sha256:' + 'f' * 64
            return data
        self.helper.docker_json = changed
        with self.assertRaises(BOOT.Blocked):
            self.recover()
        self.assertFalse((self.directory / 'runtime-identity.json').exists())
        for status in ('pending', 'failed'):
            self.current['status'] = status
            self.write_manifest()
            with self.assertRaises(BOOT.Blocked):
                BOOT.approved_identity(self.app)

    def test_exact_failed_incident_requires_all_frozen_files_and_preserves_them(self):
        self.current.update(status='failed', candidate_commit=BOOT.FAILED_COMMIT,
                            image_digest=BOOT.FAILED_DIGEST,
                            image_ref='ghcr.io/a3225865526-wq/clearapi@' + BOOT.FAILED_DIGEST,
                            deployment_attempt={'status': 'failed'})
        self.write_manifest()
        for name in ('pg03-target-manifest.json', 'controlled-launch-state.json'):
            (self.app / name).write_text('{}')
        before = {p.name: p.read_bytes() for p in self.app.iterdir()}
        frozen = {name: hashlib.sha256(raw).hexdigest() for name, raw in before.items()}
        with patch.object(BOOT, 'FROZEN', frozen):
            self.assertEqual(BOOT.approved_identity(self.app)[1], BOOT.FAILED_COMMIT)
            (self.app / 'controlled-launch-state.json').write_text('{"stage":"changed"}')
            with self.assertRaises(BOOT.Blocked):
                BOOT.approved_identity(self.app)
        self.assertEqual((self.app / 'release-manifest.json').read_bytes(), before['release-manifest.json'])

    def test_stop_timeout_lock_contention_and_release_change_do_not_publish(self):
        self.directory.mkdir(mode=0o750)
        self.running = False
        with self.assertRaises(BOOT.Blocked):
            self.recover()
        lock = self.root / 'deployment.lock'
        with BOOT.deployment_lock(lock):
            with self.assertRaises(BlockingIOError):
                with BOOT.deployment_lock(lock):
                    self.fail('second writer acquired lock')
        self.running = True
        expected = BOOT.approved_identity(self.app)
        with patch.object(BOOT, 'approved_identity', return_value=(REF, 'f' * 40, IMAGE_ID)):
            with self.assertRaises(BOOT.Blocked):
                BOOT.reconcile(self.helper, expected, self.directory, wait_seconds=0)
        self.assertFalse((self.directory / 'runtime-identity.json').exists())

    def test_installer_rejects_conflicting_asset_before_writing_any_other_asset(self):
        absent = self.root / 'absent.conf'
        conflict = self.root / 'conflict.conf'
        conflict.write_text('operator-owned content')
        with patch.object(BOOT, 'ASSETS', {str(absent): 'expected\n', str(conflict): 'different\n'}):
            with self.assertRaises(BOOT.Blocked):
                BOOT.check_assets()
        self.assertFalse(absent.exists())
        self.assertEqual(conflict.read_text(), 'operator-owned content')

    def test_installed_gate_checks_loaded_ordering_and_enablement_without_creating_runtime(self):
        script = self.root / 'runtime-boot-recovery.py'
        script.write_bytes((SCRIPTS / 'runtime-boot-recovery.py').read_bytes())
        names = ('docker.service', 'clearapi-runtime-directory.service', 'clearapi-runtime-identity.service')
        values = {name: dict(LoadState='loaded', NeedDaemonReload='no', DropInPaths='',
                            FragmentPath='/etc/systemd/system/' + name) for name in names}
        values[names[0]].update(After=names[1], Requires=names[1],
                                DropInPaths='/etc/systemd/system/docker.service.d/clearapi-runtime.conf')
        values[names[1]].update(After='systemd-tmpfiles-setup.service',
                                Requires='systemd-tmpfiles-setup.service', Before='docker.service')
        values[names[2]].update(After='docker.service clearapi-runtime-directory.service',
                                UnitFileState='enabled')
        def command(args, **kwargs):
            self.assertEqual(args[:2], ['systemctl', 'show'])
            return subprocess.CompletedProcess(args, 0, '\n'.join(k + '=' + v for k, v in values[args[2]].items()))
        with patch.object(BOOT, '__file__', str(script)), patch.object(BOOT, 'SCRIPT', str(script)), \
                patch.object(BOOT, 'load_helper'), patch.object(BOOT, 'approved_identity'), \
                patch.object(BOOT, 'directory_state', return_value='missing'), \
                patch.object(BOOT, 'check_assets', return_value='ready'):
            BOOT.check_installed(command)
            cases = ((names[2], 'UnitFileState', 'disabled'),
                     (names[0], 'NeedDaemonReload', 'yes'),
                     (names[0], 'Requires', ''), (names[1], 'DropInPaths', '/unexpected.conf'))
            for unit, key, value in cases:
                with self.subTest(unit=unit, key=key):
                    original = values[unit].get(key)
                    values[unit][key] = value
                    with self.assertRaises(BOOT.Blocked):
                        BOOT.check_installed(command)
                    if original is None:
                        del values[unit][key]
                    else:
                        values[unit][key] = original
        self.assertFalse(self.directory.exists())

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd-analyze unavailable')
    def test_systemd_boot_ordering_verifies_without_starting_services(self):
        unit_dir = self.root / 'units'
        unit_dir.mkdir()
        for name, content in BOOT.ASSETS.items():
            if name.endswith('.service'):
                (unit_dir / Path(name).name).write_text(content)
            elif 'docker.service.d' in name:
                dropin = unit_dir / 'docker.service.d'
                dropin.mkdir()
                (dropin / Path(name).name).write_text(content)
        (unit_dir / 'docker.service').write_text('[Service]\nType=oneshot\nExecStart=/bin/true\n')
        result = subprocess.run(['systemd-analyze', 'verify', '--man=no',
                                 str(unit_dir / 'docker.service'),
                                 str(unit_dir / 'clearapi-runtime-directory.service'),
                                 str(unit_dir / 'clearapi-runtime-identity.service')],
                                text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == '__main__':
    unittest.main()
