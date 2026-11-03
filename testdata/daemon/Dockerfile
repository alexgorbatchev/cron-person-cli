# Disposable Debian cron for the opt-in daemon integration test.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends cron \
 && rm -rf /var/lib/apt/lists/* \
 && sed -i '/pam_loginuid/s/^/#/' /etc/pam.d/cron \
 && useradd --create-home tester
CMD ["cron", "-f"]
