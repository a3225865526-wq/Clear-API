FROM docker:29-dind@sha256:9e79b879b177fbd72c34376a3dd3d91747da5db49e1f560f76aea59561471977
RUN apk add --no-cache openssh python3 && passwd -d root
RUN mkdir -p /root/.ssh /run/sshd && chmod 0700 /root/.ssh \
    && printf 'PermitRootLogin prohibit-password\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nAllowTcpForwarding no\nX11Forwarding no\n' > /etc/ssh/sshd_config
COPY start.sh /start.sh
ENTRYPOINT ["sh", "/start.sh"]
