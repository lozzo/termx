#!/bin/sh
set -eu

# 证书由宿主机持久化，Nginx 容器只读取部署副本；续期后必须显式复制并热加载。
docker run --rm \
    -v /root/nginx-proxy/conf.d/acme:/var/www/acme \
    -v /root/nginx-proxy/letsencrypt:/etc/letsencrypt \
    certbot/certbot renew --webroot -w /var/www/acme --quiet

install -d -m 755 /root/nginx-proxy/ssl/cloud.anytty.com
install -m 644 \
    /root/nginx-proxy/letsencrypt/live/cloud.anytty.com/fullchain.pem \
    /root/nginx-proxy/ssl/cloud.anytty.com/fullchain.pem
install -m 600 \
    /root/nginx-proxy/letsencrypt/live/cloud.anytty.com/privkey.pem \
    /root/nginx-proxy/ssl/cloud.anytty.com/privkey.pem

# Controller 的公网 mTLS listener 与反向代理共用同一张多 SAN 证书。
install -d -m 750 -o root -g anytty /etc/anytty/cloud/tls
install -m 644 -o root -g anytty \
    /root/nginx-proxy/letsencrypt/live/cloud.anytty.com/fullchain.pem \
    /etc/anytty/cloud/tls/controller-fullchain.pem
install -m 640 -o root -g anytty \
    /root/nginx-proxy/letsencrypt/live/cloud.anytty.com/privkey.pem \
    /etc/anytty/cloud/tls/controller-key.pem

docker exec nginx-proxy nginx -t
docker exec nginx-proxy nginx -s reload
systemctl try-restart anytty-cloud-controller.service
