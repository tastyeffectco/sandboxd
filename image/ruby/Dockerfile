# sandboxd-ruby:0.4.0 — Ruby runtime layer for Rails/SQLite apps
# (Redmine, Docuseal, Fizzy, Once-Campfire, Sessy).
FROM sandboxd-base:0.3.0
USER 0
RUN apt-get update && apt-get install -y --no-install-recommends \
      ruby-full ruby-dev bundler build-essential \
      libsqlite3-dev libyaml-dev zlib1g-dev libssl-dev libffi-dev \
      libxml2-dev libxslt1-dev pkg-config imagemagick \
    && rm -rf /var/lib/apt/lists/* && ruby --version && bundle --version
USER sandbox
