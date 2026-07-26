#!/bin/sh
set -eu

# 证书由宿主机持久化，Nginx 容器只读取部署副本；续期后必须显式复制并热加载。
docker run --rm \
    -v /root/nginx-proxy/conf.d/acme:/var/www/acme \
    -v /root/nginx-proxy/letsencrypt:/etc/letsencrypt \
    certbot/certbot renew --webroot -w /var/www/acme --quiet

install -d -m 755 /root/nginx-proxy/ssl/cloud.muxvia.com
install -m 644 \
    /root/nginx-proxy/letsencrypt/live/cloud.muxvia.com/fullchain.pem \
    /root/nginx-proxy/ssl/cloud.muxvia.com/fullchain.pem
install -m 600 \
    /root/nginx-proxy/letsencrypt/live/cloud.muxvia.com/privkey.pem \
    /root/nginx-proxy/ssl/cloud.muxvia.com/privkey.pem

docker exec nginx-proxy nginx -t
docker exec nginx-proxy nginx -s reload
