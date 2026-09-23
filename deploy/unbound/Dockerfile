# Recursive DNS resolver, for the blocklist checks.
#
# WHY: the public anti-spam lists refuse queries that arrive through a shared resolver -- Spamhaus
# answers 127.255.255.254, which means "go away", not "not listed". Every query from this deployment
# went through the datacenter's resolver, so the blocklist check could never say anything. Measured:
# through 1.1.1.1 the answer is a refusal; resolving from the root out of this VPS the same query
# returns real listing codes.
#
# So the check needs a resolver that recurses from the root on our own address, not a forwarder.
FROM alpine:3.20
RUN apk add --no-cache unbound && mkdir -p /var/lib/unbound \
    && wget -qO /var/lib/unbound/root.hints https://www.internic.net/domain/named.root
COPY unbound.conf /etc/unbound/unbound.conf
EXPOSE 53/udp 53/tcp
USER unbound
CMD ["unbound", "-d", "-c", "/etc/unbound/unbound.conf"]
