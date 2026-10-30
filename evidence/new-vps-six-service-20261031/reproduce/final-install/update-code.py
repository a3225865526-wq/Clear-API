import importlib.util,json,pathlib
spec=importlib.util.spec_from_file_location('lab',pathlib.Path(__file__).with_name('prepare-lab.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
for name in ['local-deploy-vps.py','local-deploy-stack.py','local-deploy-state.py']:
    dst='clearapi-local-deploy.py' if name=='local-deploy-vps.py' else name
    m.put(m.W/'scripts'/name,'/opt/clearapi/bin/'+dst);m.ex('chmod','750','/opt/clearapi/bin/'+dst)
cfg=json.loads(m.ex('cat','/opt/clearapi/config/stack-target.json'))
cfg['protected_inputs']={n:__import__('hashlib').sha256(m.ex('cat','/opt/clearapi/config/'+n)).hexdigest() for n in ['stack.env','secrets/token-key','secrets/admin-password','secrets/terms.txt','secrets/privacy.txt']}
cfg['helper_files']={n:m.sha(m.W/'scripts'/n) for n in cfg['helper_files']}
(m.R/'stack-target.json').write_text(json.dumps(cfg,indent=2))
m.put(m.R/'stack-target.json','/opt/clearapi/config/stack-target.json');m.ex('chmod','600','/opt/clearapi/config/stack-target.json')
windows=json.loads((m.R/'windows.json').read_text());windows['remoteConfigSha256']=m.sha(m.R/'stack-target.json')
(m.R/'windows.json').write_text(json.dumps(windows,indent=2))
m.put(m.W/'deploy/sub2api-low-resource/stack.sudoers.example','/etc/sudoers.d/clearapi-stack');m.ex('chmod','440','/etc/sudoers.d/clearapi-stack');m.ex('visudo','-cf','/etc/sudoers.d/clearapi-stack')
print('installed helper hashes refreshed')
