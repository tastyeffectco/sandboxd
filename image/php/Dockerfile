# syntax=docker/dockerfile:1.6
# sandboxd-php:0.4.0 — PHP runtime layer over the base image.
# Unlocks PHP/SQLite catalog apps (Dokuwiki, FreshRSS, Grocy, Heimdall,
# MediaWiki, Organizr, PrivateBin, Shlink, Vvveb, ClassicPress, Drupal).
# PHP apps run with the built-in server: `php -S 0.0.0.0:3000 -t <docroot>`.
FROM sandboxd-base:0.3.0
USER 0
RUN apt-get update && apt-get install -y --no-install-recommends \
      php-cli php-sqlite3 php-mysql php-pgsql php-xml php-mbstring php-gd \
      php-curl php-zip php-intl php-bcmath php-gmp php-ldap php-soap \
    && rm -rf /var/lib/apt/lists/* \
    && php --version
USER sandbox
