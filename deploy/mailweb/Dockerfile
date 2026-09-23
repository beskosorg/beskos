# Minimal image for CapRover's `mail` app: it exists only so the mail.beskos.com name has a
# valid certificate and responds to mail client discovery. See nginx.conf.
FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
