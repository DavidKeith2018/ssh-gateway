FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-server procps curl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --shell /bin/bash demo \
 && echo 'demo:demo-ssh-password' | chpasswd \
 && mkdir -p /run/sshd /srv/app \
 && chown demo:demo /srv/app \
 && printf '\nPasswordAuthentication yes\nPermitRootLogin no\nUsePAM no\nPrintMotd no\nPrintLastLog no\n' >> /etc/ssh/sshd_config \
 && printf 'cd /srv/app\nexport PS1="\\[\\e[32m\\]\\u@\\h\\[\\e[0m\\]:\\[\\e[34m\\]\\w\\[\\e[0m\\]\\$ "\n' > /home/demo/.bashrc \
 && chown demo:demo /home/demo/.bashrc
EXPOSE 22
CMD ["sh", "-c", "rm -f /etc/ssh/ssh_host_* && ssh-keygen -A && exec /usr/sbin/sshd -D -e -h /etc/ssh/ssh_host_ed25519_key"]
