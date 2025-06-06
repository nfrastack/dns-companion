## 1.1.1 2025-05-30 <code at nfrastack dot com>

   ### Changed
     - Cleaned Tailscale Filter compilation warnings to allow nix flake to build.

## 1.1.0 2025-05-30 <code at nfrastack dot com>

Major drop of features for this release including some ways to output records to various file formats, the ability to connect to more reverse proxies, vpn providers like tailscale and zerotier making this into a DNS manager suitable for modern infrastructure.
Reach out if you have DNS servers with API support that I can have access to and I'll start building support for the next release..

   ### Added
     - Add log_level VERBOSE sitting in the middle of debug and info. This is the new default if not explicit in config.
     - Add scoped logging to each poller, dns provider, domain configuration, output provider - log_level will override per provider
     - Add support for all network based pollProviders to supply their own TLS ca,cert, and keys. Also, ability to disable certificate verification.
     - (poll) Added File provider to read YAML/JSON/Hosts/Zonefile from filesystem with customizable interval to poll for changes or ondemand/fsnotify
     - (poll) Added Remote provider to read YAML/JSON/Hosts/Zonefile from a HTTP/HTTPS source with basic authentication supported
     - (poll) Added Zerotier Poll provider to poll for nodes in a Zerotier Central or ZT-Net (Self hosted) network
     - (poll) Added Tailscale Poll provider to poll for devices in a Tailscale tailnet or Headscale network
     - (poll) Added Caddy Poll provider to read host.domains from Caddy Admin API
     - (dns) support multiple providers
     - (output) Add functionality to output records to various files (hosts, json, yaml, zone)
     - (output/hosts) auto flatten cnames to accomodate for deficiencies in host file format
     - (output) implement smart %template% logic for filename writing

   ### Changed
     - Created pollCommonfunctions for poll providers (http, records management, options, processing of parsed,received data, filter logic for easier implementation of future pollers)
     - If docker PollProvider detects that it is Podman running then log it, and also throw errors if Podman is used for Swarm mode
     - Many log entries from DEBUG -> VERBOSE

   ### Fixed
     - Issue where record targets weren't being read correctly with the traefik poll provider


## 1.0.0 2025-05-23 <code at nfrastack dot com>

Inaugral release of the DNS Companion!
This tool will augment the amazing capabilities of working with the various pollers (eg Docker and Traefik) with hostname entries and perform DNS operations on providers such as Cloudflare.
This is an evolution from the tiredofit/docker-traefik-cloudflare-companion tool built and maintained in Python. This Go developed tool hopefully provides a more modular, single binary approach
that can run in a container environment, via the command line or via systemd. It is planned to introduce more polling providers and DNS provider support in the near future.

There has been a large amount of work performed to provide feature parity to the formerly mentioned python based tool, along with some new additions of features and other quality of life improvements.

   ### Added
      - (config) YAML based configuration with include file support
      - (config) Can load multiple config files via the command line
      - (config) Environment based configuration ovverides of config file
      - (poll) 2 polling providers provided (docker,traefik)
      - (poll) multiple poll provider capability
      - (poll/docker) supports reading container labels from traefik.router.host labels (including complex multiple host rules)
      - (poll/docker) support overriding via nfrastack.dns labels to define different targets, records, ttl, or to disable processing
      - (poll/docker) supports tls, http, socket support for connecting to docker host
      - (poll/docker) support docker swarm mode
      - (poll/docker) support for processing existing running containers, or wait until new events occur
      - (poll/docker) option to remove dns records when container/service stops
      - (poll/traefik) reads from Traefik (2.x.x and up - tested up to 3.4.x) API
      - (poll/traefik) supports mutliple host and wildcards
      - (poll/traefik) configurable polling interval
      - (poll/traefik) process existing routers, or wait for new events
      - (poll/traefik) option to remove dns records when router disapepars from configuration
      - Filters (poll/traefik) Ability to filter routers by name, service, provider, entrypoint, status, rule, none
      - Filters (poll/docker) Ability to filter containers by label, name, network, image, service, health, none)
      - Filters can be chained with operators AND, OR, NOT, and negation
      - Filters support wildcard and regular expressions
      - (providers) 1 provider provided (cloudflare)
      - (providers) utilize differnet provider profiles for different config purposes
      - (providers) support A, AAAA, CNAME create, read, update records. Including smart autodetection if not specified.
      - (providers) support checking if record exists and updating as
      - (providers) support multiple A, AAAA records
      - (providers) include/exclude processing certain subdomains
      - (provider/cloudflare) support global api email+key or scoped tokens
      - (provider/cloudflare) support proxied mode
      - Sparse (info) or rich (debug) or TMI (trace) logging
      - Ability to execute without performing changes (dry-run)
      - Support enabling Multicast DNS support
      - Single Binary, runs on amd64 and aarch64
      - Sample configuration files included
      - Docker image included
      - NixOS Module included
