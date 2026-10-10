# Used by GoReleaser (dockers_v2 in .goreleaser.yml), which puts each
# platform's prebuilt static binary at $TARGETPLATFORM/bmdctl in the build
# context. Not meant for `docker build` from a clone. A switcher is reached
# over UDP on the local network, so run the image with --network host; to
# run the engine (`bmdctl engine`), set BMDCTL_SOCKET to a path on a volume
# the clients share (see README.md).
FROM scratch
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/bmdctl /bmdctl
ENTRYPOINT ["/bmdctl"]
