FROM ghcr.io/goreleaser/goreleaser-cross:v1.24 AS goreleaser

RUN dpkg --remove-architecture ppc64el || true

# Install X11 headers/libs needed by clipboard dependency
RUN apt-get update -y && \
    apt-get install -y --no-install-recommends \
        xorg-dev libx11-dev libxfixes-dev libxi-dev libxrender-dev libxtst-dev \
    && rm -rf /var/lib/apt/lists/*
