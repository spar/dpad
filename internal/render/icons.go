package render

import "strings"

// Icon modes for the icons: setting. Auto shows icons only in terminals that
// ship Nerd Font glyphs themselves, so nobody sees boxes for a missing font.
const (
	IconsAuto = "auto"
	IconsNerd = "nerd"
	IconsOff  = "off"
)

var IconModes = []string{IconsAuto, IconsNerd, IconsOff}

var icons bool

// SetIcons turns the icons before container names on or off.
func SetIcons(on bool) {
	if on != icons {
		icons = on
		generation++ // cached rows were drawn with the other setting
	}
}

func IconsOn() bool { return icons }

// Nerd Font glyphs, named as in Nerd Fonts' cheat sheet. Every one is in
// Nerd Fonts 3.0: terminals bundle older symbol fonts than the latest
// (WezTerm's releases predate the 3.3 devicons such as nf-dev-grafana), and a
// newer glyph would show as a box there.
const (
	nfCube       = '\U000f01a7' // md-cube_outline, for images without a glyph
	nfPostgres   = '\ue76e'     // dev-postgresql
	nfRedis      = '\ue76d'     // dev-redis
	nfMySQL      = '\ue704'     // dev-mysql
	nfSeal       = '\U000f047a' // md-seal, MariaDB's logo
	nfMongo      = '\ue7a4'     // dev-mongodb
	nfCouch      = '\ue7a2'     // dev-couchdb
	nfCassandra  = '\ue789'     // dev-cassandra
	nfDatabase   = '\U000f01bc' // md-database
	nfSearch     = '\U000f0349' // md-magnify
	nfNginx      = '\ue776'     // dev-nginx
	nfApache     = '\ue72b'     // dev-apache
	nfWeb        = '\U000f059f' // md-web
	nfPython     = '\ue73c'     // dev-python
	nfNode       = '\ue719'     // dev-nodejs
	nfGo         = '\ue724'     // dev-go
	nfJava       = '\ue738'     // dev-java
	nfRuby       = '\ue739'     // dev-ruby
	nfPHP        = '\ue73d'     // dev-php
	nfRust       = '\ue7a8'     // dev-rust
	nfDotnet     = '\ue77f'     // dev-dotnet
	nfErlang     = '\ue7b1'     // dev-erlang
	nfElixir     = '\ue62d'     // custom-elixir
	nfDart       = '\ue798'     // dev-dart
	nfAlpine     = '\uf300'     // linux-alpine
	nfUbuntu     = '\uf31b'     // linux-ubuntu
	nfDebian     = '\uf306'     // linux-debian
	nfFedora     = '\uf30a'     // linux-fedora
	nfArch       = '\uf303'     // linux-archlinux
	nfCentOS     = '\uf304'     // linux-centos
	nfRedHat     = '\uf316'     // linux-redhat
	nfOpenSUSE   = '\uf314'     // linux-opensuse
	nfAlma       = '\uf31d'     // linux-almalinux
	nfConsole    = '\U000f018d' // md-console
	nfDocker     = '\ue7b0'     // dev-docker
	nfMdDocker   = '\U000f0868' // md-docker
	nfKubernetes = '\U000f10fe' // md-kubernetes
	nfJenkins    = '\ue767'     // dev-jenkins
	nfGitLab     = '\uf296'     // fa-gitlab
	nfGit        = '\U000f02a2' // md-git
	nfKafka      = '\U000f100f' // md-apache_kafka
	nfRabbit     = '\U000f0907' // md-rabbit
	nfChart      = '\U000f012a' // md-chart_line
	nfFire       = '\U000f0238' // md-fire, Prometheus's logo
	nfWordPress  = '\ue70b'     // dev-wordpress
	nfDrupal     = '\ue742'     // dev-drupal
	nfPiHole     = '\U000f0df1' // md-pi_hole
	nfHome       = '\U000f07d0' // md-home_assistant
	nfShield     = '\U000f099d' // md-shield_lock
	nfPlex       = '\U000f06ba' // md-plex
	nfFilm       = '\U000f0230' // md-filmstrip
	nfCloud      = '\U000f015f' // md-cloud
	nfPhotos     = '\U000f02f9' // md-image_multiple
	nfVPN        = '\U000f0582' // md-vpn
	nfPulse      = '\U000f05f6' // md-heart_pulse
)

var imageIcons = map[string]rune{
	"postgres": nfPostgres, "postgresql": nfPostgres, "postgis": nfPostgres, "timescaledb": nfPostgres, "pgvector": nfPostgres,
	"redis": nfRedis, "mysql": nfMySQL, "mariadb": nfSeal, "mongo": nfMongo, "mongodb": nfMongo,
	"couchdb": nfCouch, "cassandra": nfCassandra,
	"valkey": nfDatabase, "memcached": nfDatabase, "influxdb": nfDatabase, "clickhouse": nfDatabase,
	"elasticsearch": nfSearch, "opensearch": nfSearch, "meilisearch": nfSearch,

	"nginx": nfNginx, "openresty": nfNginx, "nginx-proxy-manager": nfNginx,
	"httpd": nfApache, "apache": nfApache,
	"caddy": nfWeb, "traefik": nfWeb, "haproxy": nfWeb, "envoy": nfWeb,

	"python": nfPython, "node": nfNode, "golang": nfGo,
	"openjdk": nfJava, "eclipse-temurin": nfJava, "amazoncorretto": nfJava, "maven": nfJava, "gradle": nfJava, "tomcat": nfJava,
	"ruby": nfRuby, "php": nfPHP, "rust": nfRust, "dotnet": nfDotnet, "erlang": nfErlang, "elixir": nfElixir, "dart": nfDart,

	"alpine": nfAlpine, "ubuntu": nfUbuntu, "debian": nfDebian, "fedora": nfFedora, "archlinux": nfArch,
	"centos": nfCentOS, "redhat": nfRedHat, "ubi8": nfRedHat, "ubi9": nfRedHat, "opensuse": nfOpenSUSE, "almalinux": nfAlma,
	"busybox": nfConsole,

	"docker": nfDocker, "registry": nfDocker, "portainer": nfMdDocker,
	"kubernetes": nfKubernetes, "kindest/node": nfKubernetes, "k3s": nfKubernetes,
	"jenkins": nfJenkins, "gitlab": nfGitLab, "gitea": nfGit, "forgejo": nfGit,
	"kafka": nfKafka, "cp-kafka": nfKafka, "rabbitmq": nfRabbit,
	"grafana": nfChart, "prometheus": nfFire,
	"wordpress": nfWordPress, "drupal": nfDrupal,

	"pihole": nfPiHole, "home-assistant": nfHome, "homeassistant": nfHome,
	"vaultwarden": nfShield, "bitwarden": nfShield, "plex": nfPlex, "pms-docker": nfPlex,
	"jellyfin": nfFilm, "emby": nfFilm, "nextcloud": nfCloud,
	"immich": nfPhotos, "immich-server": nfPhotos, "immich-machine-learning": nfPhotos,
	"wireguard": nfVPN, "wg-easy": nfVPN, "uptime-kuma": nfPulse,
}

var imageSuffixes = []string{"-alpine", "-slim", "-server", "-ce", "-ee", "-oss"}

// ImageIcon is the glyph for image and a space, to go before a container
// name, or "" when icons are off.
func ImageIcon(image string) string {
	if !icons {
		return ""
	}
	if r, ok := lookupIcon(image); ok {
		return fg(active.UI.Accent.C()).Render(string(r)) + " "
	}
	return Dim.Render(string(nfCube)) + " "
}

// lookupIcon reads image the way Docker names images: an optional registry
// host, then namespace/name, then :tag or @digest. The name decides first,
// then the namespace, so linuxserver/plex is Plex and grafana/loki is Grafana.
func lookupIcon(image string) (rune, bool) {
	ref, _, _ := strings.Cut(strings.ToLower(image), "@")
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		ref = ref[:i]
	}
	parts := strings.Split(strings.ReplaceAll(ref, "_", "-"), "/")
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		parts = parts[1:]
	}
	name := parts[len(parts)-1]
	names := []string{name}
	for _, s := range imageSuffixes {
		if trimmed, ok := strings.CutSuffix(name, s); ok {
			names = append(names, trimmed)
		}
	}
	if len(parts) > 1 {
		ns := parts[len(parts)-2]
		names = append([]string{ns + "/" + name}, append(names, ns)...)
	}
	for _, n := range names {
		if r, ok := imageIcons[n]; ok {
			return r, true
		}
	}
	return 0, false
}
