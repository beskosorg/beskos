# MTA-STS policy server with on-demand certificates.
#
# Replaces the fixed-certificate nginx that served only our own domain. The policy has to be
# fetchable at https://mta-sts.<customer-domain>/, with a certificate valid for THAT name -- so a
# certificate exists per customer domain and can only be issued when the first sender arrives.
#
# WHY NOT ASK CAPROVER TO ISSUE THEM: that would mean giving the backend credentials that can
# reconfigure the whole platform, to add a DNS name. Here the authority runs the other way round:
# Caddy asks our API whether a hostname belongs to a customer, and our API never gains any power it
# did not already have.
#
# WHY IT RUNS ON A WORKER NODE: CapRover's nginx owns ports 80 and 443 on the leader, in host mode,
# so those ports are free on every other node. This binds them there and the DNS points at that node.
FROM caddy:2-alpine
COPY Caddyfile /etc/caddy/Caddyfile
COPY mta-sts.txt /srv/mta-sts.txt
# 443 only. Port 80 belongs to CapRover's nginx on this cluster, and nothing here needs it: the
# certificate challenge is TLS-ALPN and senders fetch the policy over HTTPS by definition.
EXPOSE 443
