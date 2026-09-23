# Backup service: pg_dump + rclone, three times a day.
#
# Its own small image instead of putting pg_dump in the API image: they're different
# responsibilities, and a failure here shouldn't be able to bring down the application (nor the
# other way around).
FROM alpine:3.20

# postgresql16-client: the client version MUST be >= the server's, or pg_dump refuses to export
# (production is on pg16).
#
# age: encrypts the copies before they leave here. It lives in the same image restore.sh uses, so
# the tool that decrypts is exactly the one that encrypted — discovering a version mismatch on
# restore day is the nightmare this service exists to avoid.
RUN apk add --no-cache bash postgresql16-client rclone curl tzdata age

COPY backup.sh /usr/local/bin/backup.sh
COPY restore.sh /usr/local/bin/restore.sh
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/backup.sh /usr/local/bin/restore.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
