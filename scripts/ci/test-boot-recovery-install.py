#!/usr/bin/env python3
"""Local transaction regression: temporary Linux filesystem, no real systemd calls."""
import base64
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
spec = importlib.util.spec_from_file_location('boot_install', Path(__file__).resolve().parents[1] / 'boot-recovery-install.py')
INSTALL = importlib.util.module_from_spec(spec)
spec.loader.exec_module(INSTALL)


class FakeSystemd:
    """Projects fixture files; every write call is simulated and recorded."""
    def __init__(self, root):
        self.root = root
        self.calls = []
        self.reload_failures = set()
        self.enable_failure = False
        self.reloads = 0
        self.service_drift = False
        self.ordering_drift = False

    def state(self):
        result = {}
        for unit in INSTALL.UNITS:
            present = unit == 'docker.service' or (self.root / ('etc/systemd/system/' + unit)).is_file()
            item = dict.fromkeys(INSTALL.Systemd.properties, '')
            item.update(LoadState='loaded' if present else 'not-found', NeedDaemonReload='no',
                        UnitFileState='static' if unit == 'docker.service' else ('disabled' if present else 'not-found'),
                        ActiveState='active' if unit == 'docker.service' else 'inactive',
                        SubState='running' if unit == 'docker.service' else 'dead',
                        FragmentPath='/usr/lib/systemd/system/docker.service' if unit == 'docker.service'
                        else ('/etc/systemd/system/' + unit if present else ''))
            result[unit] = item
        if (self.root / INSTALL.CONFIG_PATHS[2].lstrip('/')).exists():
            result['docker.service'].update(Requires=INSTALL.UNITS[1], After=INSTALL.UNITS[1], DropInPaths=INSTALL.CONFIG_PATHS[2])
        if (self.root / INSTALL.CONFIG_PATHS[1].lstrip('/')).exists():
            result[INSTALL.UNITS[1]].update(Requires='systemd-tmpfiles-setup.service',
                                         After='systemd-tmpfiles-setup.service', Before='docker.service')
        if (self.root / INSTALL.UNIT_PATH.lstrip('/')).exists():
            result[INSTALL.UNIT]['After'] = 'docker.service ' + INSTALL.UNITS[1]
        if (self.root / INSTALL.LINK.lstrip('/')).is_symlink():
            result[INSTALL.UNIT]['UnitFileState'] = 'enabled'
        if self.service_drift:
            result['docker.service']['ActiveState'] = 'inactive'
        if self.ordering_drift and result[INSTALL.UNIT]['LoadState'] == 'loaded':
            result[INSTALL.UNIT]['Requires'] = 'docker.service'
        return result

    def reload(self):
        self.calls.append('reload')
        self.reloads += 1
        if self.reloads in self.reload_failures:
            raise OSError('simulated reload failure')

    def enable(self):
        self.calls.append('enable')
        if self.enable_failure:
            raise OSError('simulated partial enable failure')


@unittest.skipUnless(hasattr(os, 'geteuid') and os.geteuid() == 0, 'isolated Linux root required')
class TransactionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='clearapi-install-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'host'
        self.root.mkdir(mode=0o700)
        for directory in ('opt/clearapi/bin', 'etc/tmpfiles.d', 'etc/systemd/system', 'run/lock', 'run/systemd/system'):
            (self.root / directory).mkdir(parents=True, mode=0o755)
        self.archive = self.root / 'preinstall'
        self.artifacts = {name: dict(raw=('fixture payload for ' + name + '\n').encode(),
                                     mode=0o750 if name in INSTALL.SCRIPT_PATHS else 0o644)
                          for name in INSTALL.TARGETS}
        self.systemd = FakeSystemd(self.root)
        self.tx = INSTALL.Transaction(self.root, self.archive, self.artifacts, self.systemd)
        # Any accidental use of the real command adapter is a test failure.
        guard = patch.object(INSTALL.subprocess, 'run', side_effect=AssertionError('real command forbidden'))
        self.real_commands = guard.start()
        self.addCleanup(guard.stop)

    def put(self, name, raw=None, mode=None):
        path = self.tx.path(name)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(self.artifacts[name]['raw'] if raw is None else raw)
        path.chmod(self.artifacts[name]['mode'] if mode is None else mode)
        return path

    def finish(self):
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        self.tx.install(digest)
        return digest, baseline

    def assert_baseline(self, baseline):
        for name, saved in baseline['entries'].items():
            self.assertEqual(INSTALL.snapshot(self.tx.path(name)), saved, name)
        self.assertEqual(self.systemd.state(), baseline['systemd'])

    def test_archive_preserves_all_original_bytes_metadata_missing_and_enable_link(self):
        self.put(INSTALL.SCRIPT_PATHS[0])
        self.put(INSTALL.CONFIG_PATHS[0])
        self.tx.path(INSTALL.LINK).parent.mkdir(mode=0o755)
        self.tx.path(INSTALL.LINK).symlink_to(INSTALL.UNIT_PATH)
        before = self.tx.capture()
        digest = self.tx.prepare()
        saved = INSTALL.verify_archive(self.archive, digest)
        self.assertEqual(saved, before)
        entry = saved['entries'][INSTALL.SCRIPT_PATHS[0]]
        self.assertEqual(base64.b64decode(entry['data']), self.artifacts[INSTALL.SCRIPT_PATHS[0]]['raw'])
        self.assertEqual((entry['mode'], entry['uid'], entry['gid']), (0o750, 0, 0))
        self.assertEqual(saved['entries'][INSTALL.SCRIPT_PATHS[1]], {'kind': 'absent'})
        self.assertEqual(saved['entries'][INSTALL.LINK]['target'], INSTALL.UNIT_PATH)
        self.assertEqual(self.systemd.calls, [])

    def test_success_exact_modes_despite_umask_then_explicit_rollback(self):
        old_umask = os.umask(0o077)
        try:
            digest, baseline = self.finish()
        finally:
            os.umask(old_umask)
        for name, item in self.artifacts.items():
            self.assertEqual(self.tx.path(name).read_bytes(), item['raw'])
            info = self.tx.path(name).stat()
            self.assertEqual((info.st_uid, info.st_gid, info.st_mode & 0o7777), (0, 0, item['mode']))
        self.assertEqual(self.systemd.calls, ['reload', 'enable'])
        self.tx.rollback(digest)
        self.assert_baseline(baseline)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rolled_back')
        self.real_commands.assert_not_called()

    def test_preexisting_verified_assets_and_enable_link_are_never_modified(self):
        for name in self.artifacts:
            self.put(name)
        self.tx.path(INSTALL.LINK).parent.mkdir(mode=0o755)
        self.tx.path(INSTALL.LINK).symlink_to(INSTALL.UNIT_PATH)
        digest, baseline = self.finish()
        self.tx.rollback(digest)
        self.assert_baseline(baseline)
        self.assertFalse(any(row['event'] == 'created' for row in self.tx.journal()))

    def test_archive_failure_prevents_any_installation(self):
        with patch.object(INSTALL, 'durable_create', side_effect=OSError('disk full')):
            with self.assertRaises(OSError):
                self.tx.prepare()
        with self.assertRaises((OSError, INSTALL.Blocked)):
            self.tx.install('0' * 64)
        self.assertTrue(all(not self.tx.path(name).exists() for name in self.artifacts))
        self.assertEqual(self.systemd.calls, [])

    def test_archive_seal_and_original_payload_corruption_are_rejected(self):
        digest = self.tx.prepare()
        path = self.archive / 'baseline.json'
        baseline = json.loads(path.read_bytes())
        baseline['entries'][INSTALL.SCRIPT_PATHS[0]] = dict(kind='file', uid=0, gid=0, mode=0o750,
            dev=1, ino=1, nlink=1, sha256='0' * 64, size=3, data=base64.b64encode(b'bad').decode())
        modified = INSTALL.encode(baseline)
        path.write_bytes(modified)
        with self.assertRaises(INSTALL.Blocked):
            self.tx.install(digest)
        new_digest = INSTALL.sha(modified)
        (self.archive / 'baseline.sha256').write_text(new_digest + '\n')
        with self.assertRaisesRegex(INSTALL.Blocked, 'payload integrity'):
            INSTALL.verify_archive(self.archive, new_digest)
        self.assertEqual(self.systemd.calls, [])

    def test_conflict_permissions_owner_symlink_hardlink_and_xattr_stop_before_archive(self):
        name = INSTALL.SCRIPT_PATHS[0]
        cases = ('content', 'mode', 'owner', 'symlink', 'hardlink', 'xattr')
        for case in cases:
            with self.subTest(case=case):
                path = self.put(name)
                extra = self.root / 'unrelated'
                if case == 'content':
                    path.write_bytes(b'unknown original')
                elif case == 'mode':
                    path.chmod(0o644)
                elif case == 'owner':
                    os.chown(path, 1234, 0)
                elif case == 'symlink':
                    path.unlink()
                    path.symlink_to(extra)
                elif case == 'hardlink':
                    os.link(path, extra)
                else:
                    os.setxattr(path, 'user.review-required', b'yes')
                with self.assertRaises(INSTALL.Blocked):
                    self.tx.prepare()
                self.assertFalse(self.archive.exists())
                self.assertEqual(self.systemd.calls, [])
                path.unlink()
                if extra.exists():
                    extra.unlink()

    def test_partial_install_error_removes_only_journaled_new_files(self):
        original = self.put(INSTALL.SCRIPT_PATHS[0])
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        create = self.tx.create
        def fail_after_create(name, *args, **kwargs):
            create(name, *args, **kwargs)
            if name == INSTALL.SCRIPT_PATHS[2]:
                raise OSError('simulated interruption after third script')
        with patch.object(self.tx, 'create', side_effect=fail_after_create), self.assertRaises(OSError):
            self.tx.install(digest)
        self.assert_baseline(baseline)
        self.assertEqual(original.read_bytes(), self.artifacts[INSTALL.SCRIPT_PATHS[0]]['raw'])
        self.assertEqual(self.tx.journal()[-1]['event'], 'rolled_back')

    def test_partial_stage_write_failure_does_not_publish_incomplete_file(self):
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        write = INSTALL.durable_create
        def incomplete(path, raw, mode=0o600):
            if path.name.startswith('stage-'):
                write(path, raw[:5], mode)
                raise OSError('partial staging write')
            return write(path, raw, mode)
        with patch.object(INSTALL, 'durable_create', side_effect=incomplete), self.assertRaises(OSError):
            self.tx.install(digest)
        self.assert_baseline(baseline)

    def test_interruption_after_atomic_publish_before_stage_unlink_is_recoverable(self):
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        sync = INSTALL.sync_directory
        failed = False
        def interrupt(path):
            nonlocal failed
            if not failed and path == self.tx.path(INSTALL.SCRIPT_PATHS[0]).parent:
                failed = True
                raise OSError('interrupted after hardlink publication')
            sync(path)
        with patch.object(INSTALL, 'sync_directory', side_effect=interrupt), self.assertRaises(OSError):
            self.tx.install(digest)
        self.assertTrue(failed)
        self.assert_baseline(baseline)

    def test_reload_failure_stops_enable_and_verifies_one_rollback_reload(self):
        self.systemd.reload_failures = {1}
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        with self.assertRaises(OSError):
            self.tx.install(digest)
        self.assertEqual(self.systemd.calls, ['reload', 'reload'])
        self.assert_baseline(baseline)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rolled_back')

    def test_enable_failure_restores_link_files_metadata_and_systemd(self):
        self.systemd.enable_failure = True
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        with self.assertRaises(OSError):
            self.tx.install(digest)
        self.assertEqual(self.systemd.calls, ['reload', 'enable', 'reload'])
        self.assert_baseline(baseline)

    def test_rollback_reload_failure_records_incomplete_and_forbids_retry(self):
        self.systemd.enable_failure = True
        self.systemd.reload_failures = {2}
        digest = self.tx.prepare()
        with self.assertRaisesRegex(INSTALL.Blocked, 'rollback incomplete'):
            self.tx.install(digest)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rollback_failed')
        before = list(self.systemd.calls)
        with self.assertRaisesRegex(INSTALL.Blocked, 'already attempted'):
            self.tx.rollback(digest)
        self.assertEqual(self.systemd.calls, before)

    def test_unknown_replacement_or_permission_drift_is_preserved_during_rollback(self):
        for case in ('replacement', 'permission'):
            with self.subTest(case=case):
                tx = INSTALL.Transaction(self.root, self.root / ('archive-' + case), self.artifacts, self.systemd)
                # Use just one new file then a separate rollback to model process recovery.
                digest = tx.prepare()
                name = INSTALL.SCRIPT_PATHS[0]
                tx.create(name, self.artifacts[name]['raw'])
                path = tx.path(name)
                if case == 'replacement':
                    path.unlink()
                    path.write_bytes(b'foreign replacement')
                    path.chmod(0o750)
                else:
                    path.chmod(0o640)
                before = path.read_bytes(), path.stat().st_mode
                with self.assertRaisesRegex(INSTALL.Blocked, 'rollback incomplete'):
                    tx.rollback(digest)
                self.assertEqual((path.read_bytes(), path.stat().st_mode), before)
                self.assertEqual(tx.journal()[-1]['event'], 'rollback_failed')
                path.unlink()

    def test_conflicting_publication_race_never_overwrites_foreign_file(self):
        digest = self.tx.prepare()
        event = self.tx.event
        def race(kind, **fields):
            event(kind, **fields)
            if kind == 'publish_intent' and fields['path'] == INSTALL.SCRIPT_PATHS[0]:
                self.put(fields['path'], b'foreign race winner')
        with patch.object(self.tx, 'event', side_effect=race), self.assertRaises(INSTALL.Blocked):
            self.tx.install(digest)
        self.assertEqual(self.tx.path(INSTALL.SCRIPT_PATHS[0]).read_bytes(), b'foreign race winner')

    def test_unproven_directory_creation_is_retained_and_reported_incomplete(self):
        digest = self.tx.prepare()
        event = self.tx.event
        def crash(kind, **fields):
            if kind == 'created' and fields['path'] == INSTALL.DIRECTORIES[0]:
                raise OSError('directory created but completion record unavailable')
            event(kind, **fields)
        with patch.object(self.tx, 'event', side_effect=crash), self.assertRaises(INSTALL.Blocked):
            self.tx.install(digest)
        self.assertTrue(self.tx.path(INSTALL.DIRECTORIES[0]).is_dir())
        self.assertEqual(self.tx.journal()[-1]['event'], 'rollback_failed')

    def test_recovery_uses_durable_journal_not_process_memory(self):
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        self.tx.create(INSTALL.SCRIPT_PATHS[0], self.artifacts[INSTALL.SCRIPT_PATHS[0]]['raw'])
        restarted = INSTALL.Transaction(self.root, self.archive, None, self.systemd)
        restarted.rollback(digest)
        self.assert_baseline(baseline)
        with self.assertRaisesRegex(INSTALL.Blocked, 'rollback-only'):
            restarted.install(digest)

    def test_journal_tamper_and_archive_reuse_block_all_further_writes(self):
        digest = self.tx.prepare()
        journal = self.archive / 'journal.jsonl'
        journal.write_bytes(journal.read_bytes().replace(b'prepared', b'forgedxx'))
        with self.assertRaisesRegex(INSTALL.Blocked, 'journal integrity'):
            self.tx.install(digest)
        with self.assertRaises(FileExistsError):
            self.tx.prepare()
        self.assertEqual(self.systemd.calls, [])

    def test_loaded_ordering_mismatch_rolls_back_instead_of_claiming_success(self):
        self.systemd.ordering_drift = True
        digest = self.tx.prepare()
        baseline = INSTALL.verify_archive(self.archive, digest)
        with self.assertRaisesRegex(INSTALL.Blocked, 'ordering mismatch'):
            self.tx.install(digest)
        self.assert_baseline(baseline)

    def test_existing_mask_alias_or_unreviewed_enable_link_is_rejected(self):
        alias = self.root / 'etc/systemd/system/other.target.wants'
        alias.mkdir()
        (alias / INSTALL.UNIT).symlink_to(INSTALL.UNIT_PATH)
        with self.assertRaisesRegex(INSTALL.Blocked, 'unit link'):
            self.tx.prepare()
        self.assertFalse(self.archive.exists())

    def test_archive_cannot_be_created_at_or_inside_managed_paths(self):
        for name in (INSTALL.DIRECTORIES[0], INSTALL.SCRIPT_PATHS[0], INSTALL.LINK):
            with self.subTest(path=name):
                tx = INSTALL.Transaction(self.root, self.tx.path(name), self.artifacts, self.systemd)
                with self.assertRaisesRegex(INSTALL.Blocked, 'archive overlaps'):
                    tx.prepare()
                self.assertFalse(self.tx.path(name).exists())

    def test_same_bytes_foreign_inode_replacement_is_not_accepted_or_overwritten(self):
        digest = self.tx.prepare()
        enable = self.systemd.enable
        def replace():
            enable()
            path = self.tx.path(INSTALL.SCRIPT_PATHS[0])
            replacement = self.root / 'foreign'
            replacement.write_bytes(path.read_bytes())
            replacement.chmod(0o750)
            os.replace(replacement, path)
        with patch.object(self.systemd, 'enable', side_effect=replace), self.assertRaisesRegex(INSTALL.Blocked, 'rollback incomplete'):
            self.tx.install(digest)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rollback_failed')
        self.assertTrue(self.tx.path(INSTALL.SCRIPT_PATHS[0]).exists())

    def test_service_state_drift_prevents_rollback_success_claim(self):
        digest = self.tx.prepare()
        def changed():
            self.systemd.calls.append('enable')
            self.systemd.service_drift = True
        with patch.object(self.systemd, 'enable', side_effect=changed), self.assertRaisesRegex(INSTALL.Blocked, 'rollback incomplete'):
            self.tx.install(digest)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rollback_failed')

    def test_untrusted_inventory_stops_before_reading_any_source_payload(self):
        source = self.root / 'missing-source'
        inventory = self.root / 'untrusted-inventory.json'
        inventory.write_text('{}')
        with self.assertRaisesRegex(INSTALL.Blocked, 'inventory identity'):
            INSTALL.payloads(inventory, source)
        self.assertEqual(self.systemd.calls, [])

    def test_baseline_drift_after_archive_stops_before_install_begin(self):
        digest = self.tx.prepare()
        path = self.put(INSTALL.SCRIPT_PATHS[0], b'unknown new arrival')
        with self.assertRaisesRegex(INSTALL.Blocked, 'unknown existing asset'):
            self.tx.install(digest)
        self.assertEqual(path.read_bytes(), b'unknown new arrival')
        self.assertEqual([row['event'] for row in self.tx.journal()], ['prepared'])
        self.assertEqual(self.systemd.calls, [])

    def test_foreign_enable_link_created_during_enable_is_not_removed(self):
        digest = self.tx.prepare()
        alias = self.root / 'etc/systemd/system/foreign.target.wants'
        alias.mkdir()
        foreign = alias / INSTALL.UNIT
        def unexpected_link():
            self.systemd.calls.append('enable')
            foreign.symlink_to(INSTALL.UNIT_PATH)
            raise OSError('simulated unexpected enable side effect')
        with patch.object(self.systemd, 'enable', side_effect=unexpected_link), self.assertRaisesRegex(INSTALL.Blocked, 'rollback incomplete'):
            self.tx.install(digest)
        self.assertTrue(foreign.is_symlink())
        self.assertEqual(os.readlink(foreign), INSTALL.UNIT_PATH)
        self.assertEqual(self.tx.journal()[-1]['event'], 'rollback_failed')

    def test_archive_truncation_unsafe_permissions_and_missing_records_are_rejected(self):
        digest = self.tx.prepare()
        path = self.archive / 'baseline.json'
        original = path.read_bytes()
        path.write_bytes(original[:-10])
        with self.assertRaises(INSTALL.Blocked):
            INSTALL.verify_archive(self.archive, digest)
        path.write_bytes(original)
        path.chmod(0o644)
        with self.assertRaisesRegex(INSTALL.Blocked, 'unsafe archive files'):
            INSTALL.verify_archive(self.archive, digest)
        path.chmod(0o600)
        changed = json.loads(original)
        del changed['entries'][INSTALL.SCRIPT_PATHS[0]]
        raw = INSTALL.encode(changed)
        path.write_bytes(raw)
        altered_digest = INSTALL.sha(raw)
        (self.archive / 'baseline.sha256').write_text(altered_digest + '\n')
        with self.assertRaisesRegex(INSTALL.Blocked, 'target set'):
            INSTALL.verify_archive(self.archive, altered_digest)

    def test_missing_nonallowlisted_parent_cannot_be_created_by_installer(self):
        (self.root / 'opt/clearapi/bin').rmdir()
        with self.assertRaisesRegex(INSTALL.Blocked, 'unreviewed missing parent'):
            self.tx.prepare()
        self.assertFalse(self.archive.exists())

    def test_deployment_lock_is_exclusive(self):
        other = INSTALL.Transaction(self.root, self.root / 'other-archive', self.artifacts, self.systemd)
        with self.tx.lock():
            with self.assertRaises(BlockingIOError), other.lock():
                pass
        self.assertEqual(self.systemd.calls, [])

    def test_systemd_adapter_command_allowlist_contains_no_start_restart_or_shell(self):
        adapter = INSTALL.Systemd()
        with patch.object(adapter, 'command', return_value='') as command:
            adapter.reload()
            adapter.enable()
        self.assertEqual([call.args for call in command.call_args_list],
                         [('daemon-reload',), ('--no-reload', 'enable', INSTALL.UNIT)])

    def test_readonly_systemd_projection_rejects_pending_jobs_or_missing_properties(self):
        adapter = INSTALL.Systemd()
        with patch.object(adapter, 'command', return_value='123 docker.service start running') as command:
            with self.assertRaisesRegex(INSTALL.Blocked, 'pending systemd jobs'):
                adapter.state()
            self.assertEqual(command.call_count, 1)
        with patch.object(adapter, 'command', side_effect=['', 'LoadState=loaded\n']):
            with self.assertRaisesRegex(INSTALL.Blocked, 'incomplete systemd projection'):
                adapter.state()

    def test_public_archive_and_install_methods_cannot_bypass_deployment_lock(self):
        other = INSTALL.Transaction(self.root, self.root / 'other-archive', self.artifacts, self.systemd)
        with other.lock():
            with self.assertRaises(BlockingIOError):
                self.tx.prepare()
        self.assertFalse(self.archive.exists())
        digest = self.tx.prepare()
        with other.lock():
            with self.assertRaises(BlockingIOError):
                self.tx.install(digest)
        self.assertEqual(self.systemd.calls, [])


@unittest.skipUnless(hasattr(os, 'geteuid') and os.geteuid() == 0, 'isolated Linux root required')
class CandidateBundleTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='clearapi-candidate-assets-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / 'source'
        self.repository.mkdir()
        scripts = Path(__file__).resolve().parents[1]
        for name in ('runtime-boot-recovery.py', 'runtime-identity-refresh.py', 'boot-recovery-install.py'):
            target = self.repository / 'scripts' / name
            target.parent.mkdir(exist_ok=True)
            shutil.copyfile(scripts / name, target)
        shutil.copyfile(scripts.parent / 'docker-compose.production.yml', self.repository / 'docker-compose.production.yml')
        self.git('init', '-q')
        self.git('-c', 'core.autocrlf=false', 'add', '.')
        self.tree = self.git('write-tree').strip()
        self.manifest = self.root / 'manifest.json'
        self.data = dict(pool_mode='off', platform='linux/amd64', image_digest='sha256:' + 'b' * 64,
                         source_tree=self.tree, source_commit=None, source_kind='git-tree-snapshot',
                         workflow_run='local-test', workflow_run_attempt=2, production_ready=False)
        self.manifest.write_text(json.dumps(self.data))
        self.output = self.root / 'bundle'
        spec = importlib.util.spec_from_file_location('bundle_builder', scripts / 'ci/build-runtime-assets.py')
        self.builder = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.builder)

    def git(self, *args):
        return subprocess.run(['git', '-C', str(self.repository), *args], check=True,
                              capture_output=True, text=True).stdout

    def build(self):
        return self.builder.build(self.repository, self.tree, self.manifest, self.output)

    def installer(self):
        spec = importlib.util.spec_from_file_location('candidate_installer', self.output / 'boot-recovery-install.py')
        candidate = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(candidate)
        return candidate

    def test_exact_tree_bundle_installs_and_rolls_back_without_starting_services(self):
        inventory = self.build()
        candidate = self.installer()
        artifacts = candidate.payloads(self.output / 'inventory.json', self.output)
        self.assertEqual(inventory['source']['root_tree'], self.tree)
        self.assertIsNone(inventory['source']['commit'])
        self.assertEqual(len(artifacts), 6)
        self.assertNotIn('/opt/clearapi/bin/pg03-failed-recovery.py', artifacts)
        host = self.root / 'host'
        host.mkdir(mode=0o700)
        for name in ('opt/clearapi/bin', 'etc/tmpfiles.d', 'etc/systemd/system', 'run/lock', 'run/systemd/system'):
            (host / name).mkdir(parents=True)
        systemd = FakeSystemd(host)
        transaction = candidate.Transaction(host, host / 'archive', artifacts, systemd)
        digest = transaction.prepare()
        before = candidate.verify_archive(transaction.archive, digest)
        transaction.install(digest)
        self.assertEqual(systemd.calls, ['reload', 'enable'])
        self.assertEqual(candidate.verify_archive(transaction.archive, digest)['rc_attempt'], 2)
        transaction.rollback(digest)
        for name, entry in before['entries'].items():
            self.assertEqual(candidate.snapshot(transaction.path(name)), entry)
        self.assertFalse((host / 'opt/clearapi/runtime').exists())

    def test_old_inventory_changed_manifest_and_helper_stop_before_installation(self):
        self.build()
        candidate = self.installer()
        historical = Path(__file__).resolve().parents[2] / 'evidence/boot-recovery-assets-supplement-rc39357150402-attempt1-20261020.json'
        with self.assertRaisesRegex(candidate.Blocked, 'inventory identity'):
            candidate.payloads(historical, self.output)
        manifest = self.output / 'gateway-image-release.json'
        original = manifest.read_bytes()
        manifest.write_text(json.dumps(dict(self.data, workflow_run_attempt=3)))
        with self.assertRaisesRegex(candidate.Blocked, 'manifest identity'):
            candidate.payloads(self.output / 'inventory.json', self.output)
        manifest.write_bytes(original)
        helper = self.output / 'scripts/runtime-identity-refresh.py'
        helper.write_bytes(helper.read_bytes() + b'\n')
        with self.assertRaisesRegex(candidate.Blocked, 'staged asset mismatch'):
            candidate.payloads(self.output / 'inventory.json', self.output)

    def test_tree_and_rc_mismatch_cannot_publish_bundle(self):
        for change in (dict(source_tree='f' * 40), dict(source_kind='git-commit'),
                       dict(workflow_run='123'), dict(pool_mode='pg03')):
            with self.subTest(change=change):
                self.manifest.write_text(json.dumps(dict(self.data, **change)))
                with self.assertRaises(ValueError):
                    self.build()
                self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main(verbosity=2)
