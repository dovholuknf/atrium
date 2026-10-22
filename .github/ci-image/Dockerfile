FROM ubuntu:24.04
ARG GO_VERSION=1.26.10
ARG PLAYWRIGHT_VERSION=1.63.0
ENV DEBIAN_FRONTEND=noninteractive \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    PATH=/usr/local/go/bin:/root/go/bin:$PATH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git gnupg build-essential jq unzip xz-utils fonts-dejavu-core \
 && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz \
 && rm -rf /var/lib/apt/lists/*
# Baked here so jobs never download the browser or its apt dependencies.
RUN mkdir /pw && cd /pw && npm init -y >/dev/null && npm install --no-audit --no-fund "@playwright/test@${PLAYWRIGHT_VERSION}" \
 && npx playwright install --with-deps chromium \
 && rm -rf /var/lib/apt/lists/* /pw
