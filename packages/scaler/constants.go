package scaler

import "time"

const (
	System    = "scalefleet-ci"
	Version   = "v1.0.0"
	CommitSHA = "NA"
)

const (
	// GitHub scale set constants.
	scaleSetNamePrefix = "scalefleet-ci-scaleset"
	vmPrefix           = "scalefleet-ci-runner"

	githubConfigURL = ""
	workFolder      = "_work"
)

const (
	// Runtime behavior.
	apiCallTimeout    = 20 * time.Second
	apiReadAttempts   = 3
	apiWriteAttempts  = 1
	apiInitialBackoff = 250 * time.Millisecond

	runnerMaxRunDuration      = 30 * time.Minute
	orphanRunnerGracePeriod   = 20 * time.Minute
	orphanRunnerSweepInterval = 1 * time.Minute
)

const (
	intentKindCreate intentKind = "create"
	intentKindDelete intentKind = "delete"
)

const (
	intentStateQueued    intentState = "queued"
	intentStateInFlight  intentState = "in_flight"
	intentStateSubmitted intentState = "submitted"
)

const (
	intentWorkerCount           = 4
	intentConfirmInterval       = 10 * time.Second
	intentUnknownStateTimeout   = 3 * time.Minute
	intentCapacityRetryCooldown = 60 * time.Second
	intentRetryBackoff          = 10 * time.Second
)

const startupScript = `#!/bin/bash
set -euo pipefail

metadata_attr() {
  local key="$1"
  curl -fsS -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/${key}"
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

require_cmd curl
require_cmd install
require_cmd tar
require_cmd awk
require_cmd runuser
require_cmd useradd
require_cmd uname
require_cmd tr

jit_config="$(metadata_attr "jit-config")"
runner_name="$(metadata_attr "runner-name")"
runner_id="$(metadata_attr "runner-id")"

runner_user="runner"
runner_install_dir="/opt/actions-runner"
runner_state_dir="/var/lib/github-runner"
runner_log_file="/var/log/github-runner.log"

if ! id -u "${runner_user}" >/dev/null 2>&1; then
  useradd --create-home --home-dir "/home/${runner_user}" --shell /bin/bash "${runner_user}"
fi

install -d -m 0700 -o "runner" -g "runner" "/var/lib/github-runner"
install -m 0600 -o "runner" -g "runner" /dev/null "/var/lib/github-runner/jit-config"
install -m 0644 -o "runner" -g "runner" /dev/null "/var/lib/github-runner/runner-name"
install -m 0644 -o "runner" -g "runner" /dev/null "/var/lib/github-runner/runner-id"

printf "%s" "${jit_config}" > "/var/lib/github-runner/jit-config"
printf "%s" "${runner_name}" > "/var/lib/github-runner/runner-name"
printf "%s" "${runner_id}" > "/var/lib/github-runner/runner-id"

case "$(uname -m)" in
  x86_64) runner_arch="x64" ;;
  aarch64|arm64) runner_arch="arm64" ;;
  *)
    echo "unsupported runner architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

latest_location="$(curl -fsSIL --retry 5 --retry-all-errors "https://github.com/actions/runner/releases/latest" \
  | awk 'tolower($1) == "location:" {print $2}' | tail -n1 | tr -d '\r')"
latest_tag="${latest_location##*/}"
if [ -z "${latest_tag}" ] || [ "${latest_tag}" = "latest" ]; then
  echo "failed to resolve latest GitHub runner release tag" >&2
  exit 1
fi
runner_version="${latest_tag#v}"
archive_name="actions-runner-linux-${runner_arch}-${runner_version}.tar.gz"
download_url="https://github.com/actions/runner/releases/download/${latest_tag}/${archive_name}"
archive_path="/tmp/${archive_name}"

rm -rf "${runner_install_dir}"
install -d -m 0755 -o "${runner_user}" -g "${runner_user}" "${runner_install_dir}"
curl -fsSL --retry 5 --retry-all-errors -o "${archive_path}" "${download_url}"
tar -xzf "${archive_path}" -C "${runner_install_dir}"
rm -f "${archive_path}"
chown -R "${runner_user}:${runner_user}" "${runner_install_dir}"

if [ -x "${runner_install_dir}/bin/installdependencies.sh" ]; then
  "${runner_install_dir}/bin/installdependencies.sh"
fi

runuser -u "${runner_user}" -- bash -lc "cd ${runner_install_dir} && exec ./run.sh --jitconfig \"\$(< ${runner_state_dir}/jit-config)\"" >>"${runner_log_file}" 2>&1 &
`

// DefaultRunnerStartupScript returns the built-in startup script used by default runner provider configs.
func DefaultRunnerStartupScript() string {
	return startupScript
}

// DefaultRunnerMaxRunDuration returns the default hard max runtime used for runner VMs.
func DefaultRunnerMaxRunDuration() time.Duration {
	return runnerMaxRunDuration
}
