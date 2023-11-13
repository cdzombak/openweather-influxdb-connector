# openweather-influxdb-connector

Write current weather conditions from OpenWeatherMap to InfluxDB.

## Usage

```text
openweather-influxdb-connector -config /path/to/config.json [-printData]
```

### Options

- `-config`: Path to the configuration JSON file. Required.
- `-visibility`: Print weather/pollution data to stdout.
- `-help`: Print help and exit.
- `-version`: Print version and exit.

### Configuration

Configuration is provided by a JSON file, which contains the following fields:

- `api_key`: Your OpenWeatherMap API key.
- `wx_measurement_name`: Name of the weather measurement to write to InfluxDB.
- `pollution_measurement_name`: Name of the pollution measurement to write to InfluxDB.
- `lat`, `lon`: The location to look up weather for.
- `influx_server`: InfluxDB server.
- `influx_bucket`: InfluxDB bucket.
- `influx_user`, `influx_password`: InfluxDB credentials.
- `influx_token`: InfluxDB token. If using a token for bucket authentication, then leave the `influx_user` and `influx_password` config fields empty.
- `influx_org`: InfluxDB organization.
- `influx_health_check_disabled`: If set to `true`, skip checking the Influx server's health before fetching weather & attempting to write to Influx.

A sample config file is included in this repository to help you get started: [`config.example.json`](https://github.com/cdzombak/openweather-influxdb-connector/blob/main/config.example.json).

### Compatibility with [ecobee_influx_connector](https://github.com/cdzombak/ecobee_influx_connector)

If the config fields `write_ecobee_wx_measurement` and `ecobee_thermostat_name` are set, the program will write the measurement `ecobee_weather` using the same field names and types as [ecobee_influx_connector](https://github.com/cdzombak/ecobee_influx_connector) writes.

This mode aims to be a bug-for-bug compatible drop in for weather measurements written by [ecobee_influx_connector](https://github.com/cdzombak/ecobee_influx_connector).

These measurements are written _in addition_ to the usual weather & pollution measurements written by openweather-influxdb-connector.

## Installation

### macOS via Homebrew

```shell
brew install cdzombak/oss/openweather-influxdb-connector
```

### Debian via apt repository

Install my Debian repository if you haven't already:

```shell
sudo apt-get install ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dist.cdzombak.net/deb.key | sudo gpg --dearmor -o /etc/apt/keyrings/dist-cdzombak-net.gpg
sudo chmod 0644 /etc/apt/keyrings/dist-cdzombak-net.gpg
echo -e "deb [signed-by=/etc/apt/keyrings/dist-cdzombak-net.gpg] https://dist.cdzombak.net/deb/oss any oss\n" | sudo tee -a /etc/apt/sources.list.d/dist-cdzombak-net.list > /dev/null
sudo apt-get update
```

Then install `openweather-influxdb-connector` via `apt-get`:

```shell
sudo apt-get install openweather-influxdb-connector
```

### Manual installation from build artifacts

Pre-built binaries for Linux and macOS on various architectures are downloadable from each [GitHub Release](https://github.com/cdzombak/mastodon-post/releases). Debian packages for each release are available as well.

### Build and install locally

```shell
git clone https://github.com/cdzombak/openweather-influxdb-connector.git
cd openweather-influxdb-connector
make build

cp out/openweather-influxdb-connector $INSTALL_DIR
```

## Docker images

Docker images are available for a variety of Linux architectures from [Docker Hub](https://hub.docker.com/r/cdzombak/mastodon-post) and [GHCR](https://github.com/cdzombak/unshorten/pkgs/container/mastodon-post). Images are based on the `scratch` image and are as small as possible.

Run them via, for example:

```shell
docker run --rm -v ./my/config.json:/config.json:ro cdzombak/openweather-influxdb-connector:1
docker run --rm -v ./my/config.json:/config.json:ro ghcr.io/cdzombak/openweather-influxdb-connector:1
```

The default Docker command is `["-config", "/config.json"]`, so you can mount your config file at that path.

## Example Usage

This runs on my home server via the following crontab entry:

```text
## Log weather and pollution to InfluxDB from OpenWeatherMap, every 10 minutes
*/10 *  *  *  *  openweather-influxdb-connector -config /home/cdzombak/.config/openweather-influxdb-connector.json
```

## About

- Issues: [github.com/cdzombak/openweather-influxdb-connector/issues](https://github.com/cdzombak/openweather-influxdb-connector/issues)
- Author: [Chris Dzombak](https://www.dzombak.com)
  - [GitHub: @cdzombak](https://www.github.com/cdzombak)

## License

MIT; see `LICENSE` in this repository.
