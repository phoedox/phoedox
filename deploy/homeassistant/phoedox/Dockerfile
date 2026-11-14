# The add-on reuses the static binaries of the Phoedox release image on top
# of the Home Assistant base image (bashio for reading the options).
ARG PHOEDOX_IMAGE=ghcr.io/phoedox/phoedox:latest
ARG BUILD_FROM
FROM ${PHOEDOX_IMAGE} AS phoedox

FROM ${BUILD_FROM}
COPY --from=phoedox /usr/local/bin/phoedox /usr/local/bin/phx /usr/local/bin/
COPY run.sh /run.sh
RUN chmod a+x /run.sh
CMD ["/run.sh"]
