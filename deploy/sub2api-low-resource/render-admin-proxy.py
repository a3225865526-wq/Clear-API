"""Render an optional TLS and IP-restricted Sub2API virtual host for the existing edge."""
import argparse
import ipaddress
import os
from pathlib import Path
import re
from urllib.parse import urlsplit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    origin = urlsplit(os.environ['SUB2API_ADMIN_URL'])
    host = os.environ['SUB2API_ADMIN_HOST']
    if not re.fullmatch(r'[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?', host):
        raise SystemExit('Invalid admin hostname')
    if (origin.scheme != 'https' or origin.hostname != host.lower() or
            origin.username is not None or origin.password is not None or
            origin.path not in ('', '/') or origin.query or origin.fragment):
        raise SystemExit('Admin URL must be the configured HTTPS origin without credentials')
    networks = [ipaddress.ip_network(item.strip(), strict=True)
                for item in os.environ['SUB2API_ADMIN_ALLOW_CIDRS'].split(',')]
    if not networks or any(network.prefixlen == 0 for network in networks):
        raise SystemExit('Explicit restricted admin CIDRs required')
    allow = '\n'.join(f'    allow {network};' for network in networks)
    server = f'''
  server {{
    listen 8443 ssl;
    server_name {host};
    ssl_certificate /etc/nginx/admin-tls/fullchain.pem;
    ssl_certificate_key /etc/nginx/admin-tls/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    if ($host != {host}) {{ return 421; }}
{allow}
    deny all;
    client_max_body_size 16m;
    location / {{
      proxy_pass http://sub2api:8080;
      proxy_http_version 1.1;
      proxy_set_header Connection "";
      proxy_set_header Host $host;
      proxy_set_header X-Forwarded-For $remote_addr;
      proxy_set_header X-Forwarded-Proto https;
      proxy_buffering off;
      proxy_read_timeout 360s;
      proxy_send_timeout 360s;
      proxy_next_upstream off;
    }}
  }}
'''
    base = (Path(__file__).parent / 'nginx.conf').read_text(encoding='utf-8')
    position = base.rfind('}')
    # Refuse to overwrite an existing generated or operator-maintained file.
    with args.output.open('x', encoding='utf-8', newline='\n') as output:
        output.write(base[:position] + server + base[position:])


if __name__ == '__main__':
    main()
