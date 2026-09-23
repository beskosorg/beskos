# MTA-STS policy server.
#
# WHY A SEPARATE APP: RFC 8461 requires the policy at https://mta-sts.<domain>/.well-known/mta-sts.txt
# and forbids redirects, so it has to be served under that exact hostname with a valid certificate.
# CapRover reserves *.beskos.com for app subdomains, which means the name has to BE an app.
#
# WHY IT IS TINY: it serves one immutable text file. Reusing the landing's Next.js server for that
# would run a Node process per request path that will only ever return four lines.
#
# THE SAME POLICY SERVES EVERY CUSTOMER DOMAIN, which is what makes this worth having: a customer
# points their MX at mail.beskos.com, so their policy names the same host as ours. Per customer the
# only work left is DNS -- a CNAME and a TXT, next to the SPF, DKIM and DMARC already published
# automatically -- plus a certificate for their mta-sts subdomain.
FROM nginx:1.27-alpine
COPY mta-sts.txt /usr/share/nginx/html/.well-known/mta-sts.txt
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
