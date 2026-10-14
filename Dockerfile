# Used by GoReleaser (dockers_v2 in .goreleaser.yml), which puts each
# platform's prebuilt static binary at $TARGETPLATFORM/bmdctl in the build
# context. Not meant for `docker build` from a clone.
#
# The image runs the engine in the foreground with the HTTP API
# (`bmdctl engine --serve`). A switcher is reached over UDP on the local
# network, so run it with --network host; the API then listens on the
# host's 127.0.0.1:8880 (no authentication: keep it there, or pass
# `engine --serve --listen ADDRESS` deliberately). hosts.yml, the log and the
# still cache live under /data; the engine's socket is in /run/bmdctl, a
# volume a host's bmdctl can share (README.md):
#
#   docker run -d --network host -v bmdctl:/data -v /run/bmdctl:/run/bmdctl \
#       ghcr.io/lucas-romanenko/bmdctl
#   BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock bmdctl status
FROM scratch
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/bmdctl /bmdctl
ENV HOME=/data XDG_CONFIG_HOME=/data/config XDG_STATE_HOME=/data/state XDG_CACHE_HOME=/data/cache \
    BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock
VOLUME ["/data", "/run/bmdctl"]
ENTRYPOINT ["/bmdctl"]
CMD ["engine", "--serve"]
