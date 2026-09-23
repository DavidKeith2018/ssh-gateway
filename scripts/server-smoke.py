#!/usr/bin/env python3
"""验证服务端压缩包无需桌面会话即可运行；仅使用临时数据和本机端口。"""
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

with tempfile.TemporaryDirectory(prefix='gateway-server-') as temporary:
    root = Path(temporary)
    with tarfile.open(sys.argv[1]) as archive:
        archive.extractall(root, filter='data')
    binary = next(root.glob('*/ssh-gateway-server'))
    data = root / 'data'
    environment = {k: v for k, v in os.environ.items() if k not in
                   ('DISPLAY', 'WAYLAND_DISPLAY', 'DBUS_SESSION_BUS_ADDRESS')}
    subprocess.run([str(binary), '-data', str(data), 'admin-password'],
                   input='server-smoke-password-123\n', text=True, check=True,
                   stdout=subprocess.DEVNULL, env=environment)
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        port = reservation.getsockname()[1]
    base = f'http://127.0.0.1:{port}'
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    with (root / 'service.log').open('w+') as log:
        process = subprocess.Popen([str(binary), '-data', str(data), '-listen',
            '127.0.0.1:0', '-web', f'127.0.0.1:{port}', 'serve'],
            env=environment, stdout=log, stderr=log)
        try:
            for _ in range(100):
                try:
                    with client.open(base, timeout=1) as response:
                        assert b'<html' in response.read()
                    break
                except OSError:
                    if process.poll() is not None:
                        raise RuntimeError('服务提前退出')
                    time.sleep(0.1)
            else:
                raise RuntimeError('等待服务超时')
            request = urllib.request.Request(base + '/api/login',
                data=json.dumps({'username': 'ssh-admin', 'password': 'server-smoke-password-123'}).encode(),
                headers={'Content-Type': 'application/json', 'Origin': base})
            with client.open(request) as response:
                assert response.status == 200
            with client.open(base + '/api/targets') as response:
                assert response.status == 200
                json.load(response)
        finally:
            process.terminate()
            try:
                code = process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise
            if code != 0:
                log.seek(0)
                raise RuntimeError(log.read())
    print('通过：无桌面环境启动、Web 页面、登录、目标列表、SIGTERM 退出')
