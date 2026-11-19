# A never-run atrium as a brand-new Linux user gets it, for first-run-demo.ps1. Built from an empty context holding
# only the linux binary: no data dir, config, identity or ~/.claude goes in.
FROM node:22-bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git socat tini procps less \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code \
 && npm cache clean --force \
 && useradd -m -s /bin/bash newuser

USER newuser
WORKDIR /home/newuser
COPY --chown=newuser:newuser atrium /home/newuser/.atrium/bin/atrium
ENV PATH=/home/newuser/.atrium/bin:$PATH

# atrium binds 127.0.0.1:7778 by default and is left that way. socat carries the published port to it, so the Host
# header the browser sends stays loopback.
EXPOSE 7920
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["sh", "-c", "socat TCP-LISTEN:7920,fork,reuseaddr TCP:127.0.0.1:7778 & exec atrium run"]
