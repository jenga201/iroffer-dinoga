#!/bin/bash
set -e

# ---------------------------------------------------------------------------
# iroffer-dinoex docker entrypoint
#
# Environment-variable configuration model:
#   * Every iroffer config directive is supported as IROFFER_<DIRECTIVE>
#     (directive name uppercased, e.g. slotsmax -> IROFFER_SLOTSMAX).
#   * Empty/unset variable = leave the directive in iroffer.config alone.
#   * Multi-value directives (lists, e.g. filedir, http_vhost, channel)
#     accept ";;"-separated values, one config line per value.
#   * IROFFER_NETWORK_COUNT=N + IROFFER_NETWORK_<n>_<DIRECTIVE> variables
#     regenerate the "network { ... }" blocks on every start.
#   * Env values are applied on EVERY container start into a marker-delimited
#     region PREPENDED to iroffer.config (so network-scoped globals bind to
#     network 0). Superseded active lines are commented out as
#     "#removed-by-env:" and restored automatically when the env var is
#     removed; original network blocks commented as "#removed-by-env-net:"
#     are restored when IROFFER_NETWORK_COUNT is unset.
#
# Legacy variables kept for compatibility:
#   IROFFER_BOT_NAME -> user_nick    (IROFFER_USER_NICK wins if set)
#   PORT_RANGE       -> tcprangestart/tcprangelimit (specific vars win)
#   HTTP_PORT        -> http_port    (IROFFER_HTTP_PORT wins if set)
#
# Tables below were generated from the config parse tables in
# src/dinoex_config.c of the iroffer-dinoex snapshot pinned by
# IROFFER_SHA256 in the Dockerfile. Regenerate with
# scripts/gen_env_tables.py after changing the pinned snapshot.
# ---------------------------------------------------------------------------

# Static in-container paths/user for deployment stability.
APP_USER="iroffer"
APP_HOME="/home/${APP_USER}"
CONFIG_DIR="${APP_HOME}/config"
LOG_DIR="${APP_HOME}/logs"
CONFIG_FILE_NAME="iroffer.config"
BOT_NAME="${IROFFER_BOT_NAME:-}"
PORT_RANGE_OVERRIDE="${PORT_RANGE:-}"
HTTP_PORT_OVERRIDE="${HTTP_PORT:-}"

# Separator for multi-value env variables.
MULTI_SEP=";;"

# Temp files tracked for EXIT-trap cleanup (set in apply_environment_to_config).
OVERRIDES_TMP=""
BLOCKS_TMP=""
CONFIG_TMP_GLOB=""

# Managed regions in iroffer.config (rewritten on every start).
OVERRIDES_BEGIN="# BEGIN IROFFER ENV OVERRIDES (managed by entrypoint.sh - do not edit)"
OVERRIDES_END="# END IROFFER ENV OVERRIDES"
NETWORKS_BEGIN="# BEGIN IROFFER ENV NETWORKS (managed by entrypoint.sh - do not edit)"
NETWORKS_END="# END IROFFER ENV NETWORKS"

# ---- BEGIN GENERATED TABLES (from iroffer-dinoex config parser, snapshot pinned in Dockerfile) ----
# Single-value directives: IROFFER_<NAME> replaces the active config line (overrides region, prepended).
SINGLE_DIRECTIVES=(
  adddir_min_size admin_job_done_file admin_job_file adminlevel adminpass announce_seperator
  announce_size announce_suffix_color atfind auth_name auto_crc_check auto_default_group
  auto_path_group autoadd_color autoadd_delay autoadd_group autoadd_sort autoadd_time
  autoaddann autoaddann_short autoignore_rate autoignore_threshold autosendpack balanced_queue
  charset creditline debug direct_config_access direct_file_access disablexdccinfo
  disk_quota dos_text_files download_completed_msg dump_all enable_nick expire_logfiles
  extend_status_line fileremove_max_packs fish_only flood_protection_rate getip_network getipfromserver
  getipfromupnp group_seperator groupsincaps hadminlevel hadminpass hide_list_info
  hide_list_stop hidelockedpacks hideos holdqueue http_access_log http_admin
  http_admin_dir http_date http_dir http_forbidden http_geoip http_index
  http_port http_search idlequeuesize ignore_duplicate_ip ignoreduplicateip ignoreuploadbandwidth
  include_subdirs local_vhost logfile logfile_httpd logfile_messages logfile_notices
  login_name loginname logmessages lognotices logrotate logstats
  lowbdwth max_find max_uploads max_upspeed maxidlequeuedperperson maxqueueditemsperperson
  maxtransfersperperson mirc_dcc64 monitor_files need_level need_voice new_trigger
  nickserv_pass no_auto_rehash no_duplicate_filenames no_find_trigger no_minspeed_on_free no_natural_sort
  no_status_chat no_status_log noannounce noautorejoin nocrc32 noduplicatefiles
  nomd5sum notifytime offline old_statefile overallmaxspeed overallmaxspeeddaydays
  overallmaxspeeddayspeed overallmaxspeeddaytime owner_nick passive_dcc passive_dcc_chat pidfile
  plaintext privmsg_encrypt privmsg_fish punishslowusers queuesize quietmode
  quit_msg reconnect_delay reminder_send_retry remove_dead_users removelostfiles requeue_sends
  respondtochannellist respondtochannellistmsg respondtochannelxdcc restrictlist restrictprivlist restrictprivlistfull
  restrictprivlistmain restrictprivlistmsg restrictsend restrictsend_delay restrictsend_timeout restrictsend_warning
  ruby_script send_batch send_listfile send_statefile send_statefile_minute server_connect_timeout
  server_send_max server_send_rate show_date_added show_group_of_pack show_list_all slotsmax
  slow_privmsg smallfilebypass spaces_in_filenames start_of_month statefile status_time_dcc_chat
  subdirs_delayed tcp_buffer_size tcp_nodelay tcprangelimit tcprangestart telnet_port
  timestampconsole transferlimits transfermaxspeed transferminspeed trashcan_dir uploaddir
  uploadmaxsize uploadminspace upnp_router usenatip user_modes user_nick
  user_realname verbose_crc32 waitafterjoin xdcclist_by_privmsg xdcclist_grouponly xdcclistfile
  xdcclistfileraw xdccremovefile xdccxmlfile
)

# Multi-value directives: IROFFER_<NAME> accepts ';;'-separated values; all active lines for the
# directive outside network blocks are removed and one line per value is added.
MULTI_DIRECTIVES=(
  adddir_exclude adddir_match adminhost autoadd_dir autoadd_group_match autoaddann_mask
  autocrc_exclude autoignore_exclude channel channel_join_raw connectionmethod downloadhost
  filedir fish_exclude_nick group_admin hadminhost headline http_allow
  http_deny http_vhost log_exclude_host log_exclude_text md5sum_exclude mime_type
  nodownloadhost periodicmsg proxyinfo server server_connected_raw server_join_raw
  telnet_allow telnet_deny telnet_vhost unlimitedhost uploadhost weblist_info
  xdcc_allow xdcc_deny
)

# Bool-typed directives (validated: value must be yes|no).
BOOL_DIRECTIVES=(
  announce_size auto_crc_check auto_default_group auto_path_group autoaddann_short balanced_queue
  direct_config_access direct_file_access disablexdccinfo dos_text_files dump_all extend_status_line
  fish_only getipfromserver getipfromupnp groupsincaps hide_list_info hide_list_stop
  hidelockedpacks hideos holdqueue http_geoip http_search ignoreduplicateip
  ignoreuploadbandwidth include_subdirs logmessages lognotices logstats mirc_dcc64
  need_voice no_auto_rehash no_duplicate_filenames no_find_trigger no_minspeed_on_free no_natural_sort
  no_status_chat no_status_log noannounce noautorejoin nocrc32 noduplicatefiles
  nomd5sum offline old_statefile passive_dcc passive_dcc_chat plaintext
  privmsg_encrypt quietmode removelostfiles requeue_sends respondtochannellist respondtochannelxdcc
  restrictlist restrictprivlist restrictprivlistfull restrictprivlistmain restrictsend restrictsend_warning
  send_batch show_date_added show_group_of_pack show_list_all spaces_in_filenames subdirs_delayed
  tcp_nodelay timestampconsole upnp_router verbose_crc32 xdcclist_by_privmsg xdcclist_grouponly
  xdcclistfileraw
)

# Network-scoped directives usable inside IROFFER_NETWORK_<n>_* blocks.
NETWORK_DIRECTIVES=(
  auth_name channel channel_join_raw connectionmethod getip_network local_vhost
  login_name need_level nickserv_pass noannounce offline plaintext
  proxyinfo respondtochannellist respondtochannelxdcc restrictlist restrictsend server
  server_connect_timeout server_connected_raw server_join_raw server_send_max server_send_rate slow_privmsg
  usenatip user_modes user_nick
)
# ---- END GENERATED TABLES ----

# Network-scoped directives that accept multiple ';;'-separated values per network.
NETWORK_MULTI_DIRECTIVES=(
  channel channel_join_raw connectionmethod proxyinfo server server_connected_raw server_join_raw
)

run_iroffer_as_app_user() {
  # Keep initialization as root, then drop privileges for the bot process.
  if [ "$(id -u)" -eq 0 ]; then
    exec runuser -u "${APP_USER}" -- /iroffer "$@"
  fi

  exec /iroffer "$@"
}

# allow arguments
if [ "${1:0:1}" = '-' ]; then
  echo "Arg:""$@"
  set -- /iroffer "$@"
fi

init_config() {
  # Config: ensure directory exists, then ensure config file exists.
  if [ ! -d "${CONFIG_DIR}" ]; then
    mkdir -p "${CONFIG_DIR}"
    chmod -R 0755 "${CONFIG_DIR}"
    chown -R "${APP_USER}": "${CONFIG_DIR}"
  fi

  if [ ! -e "${CONFIG_DIR}/${CONFIG_FILE_NAME}" ]; then
    cp /extras/sample.customized.config "${CONFIG_DIR}/${CONFIG_FILE_NAME}"
    echo "Copied fresh sample configuration to ${CONFIG_DIR}/${CONFIG_FILE_NAME}."
    chmod -R 0755 "${CONFIG_DIR}/${CONFIG_FILE_NAME}"
    chown -R "${APP_USER}": "${CONFIG_DIR}/${CONFIG_FILE_NAME}"
  fi

  # Logs
  if [ ! -d "${LOG_DIR}" ]; then
    mkdir -p "${LOG_DIR}"
    chmod -R 0755 "${LOG_DIR}"
    chown -R "${APP_USER}": "${LOG_DIR}"
  fi
}

# env var name for a directive: slotsmax -> IROFFER_SLOTSMAX
directive_env_name() {
  printf 'IROFFER_%s' "$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')"
}

# indirect value lookup: prints the value of IROFFER_<DIRECTIVE> (empty if unset)
directive_env_value() {
  local var_name
  var_name="$(directive_env_name "$1")"
  printf '%s' "${!var_name:-}"
}

# Reject values containing newlines: the config format is line-based, so a
# newline would inject additional (arbitrary) directives into iroffer.config.
validate_no_newline() {
  local var_name="$1" value="$2"
  if [[ "${value}" == *$'\n'* || "${value}" == *$'\r'* ]]; then
    echo "Invalid ${var_name}: value must not contain newlines (config format is line-based)." >&2
    exit 1
  fi
}

in_list() {
  local needle="$1"; shift
  local item
  for item in "$@"; do
    [ "${item}" = "${needle}" ] && return 0
  done
  return 1
}

# Validate PORT_RANGE (START-END, 1-65535). Sets PORT_RANGE_START/PORT_RANGE_LIMIT.
validate_port_range() {
  local range_start range_limit
  range_start=${PORT_RANGE_OVERRIDE%%-*}
  range_limit=${PORT_RANGE_OVERRIDE##*-}

  if ! [[ "${range_start}" =~ ^[0-9]+$ && "${range_limit}" =~ ^[0-9]+$ ]]; then
    echo "Invalid PORT_RANGE '${PORT_RANGE_OVERRIDE}'. Expected format: START-END (e.g. 30000-31000)." >&2
    exit 1
  fi

  if [ "${range_start}" -lt 1 ] || [ "${range_limit}" -gt 65535 ] || [ "${range_start}" -gt "${range_limit}" ]; then
    echo "Invalid PORT_RANGE '${PORT_RANGE_OVERRIDE}'. Valid range is 1-65535 and START <= END." >&2
    exit 1
  fi

  PORT_RANGE_START="${range_start}"
  PORT_RANGE_LIMIT="${range_limit}"
}

validate_http_port() {
  if ! [[ "${HTTP_PORT_OVERRIDE}" =~ ^[0-9]+$ ]]; then
    echo "Invalid HTTP_PORT '${HTTP_PORT_OVERRIDE}'. Expected a numeric port (e.g. 8080)." >&2
    exit 1
  fi

  if [ "${HTTP_PORT_OVERRIDE}" -lt 1 ] || [ "${HTTP_PORT_OVERRIDE}" -gt 65535 ]; then
    echo "Invalid HTTP_PORT '${HTTP_PORT_OVERRIDE}'. Valid range is 1-65535." >&2
    exit 1
  fi
}

# Build the override list (one "directive<TAB>value" per line, in table order)
# from all set IROFFER_* variables plus the legacy aliases.
build_overrides() {
  local overrides_file="$1"
  : > "${overrides_file}"

  local directive value part
  local -a parts

  # Legacy aliases first (specific IROFFER_* vars take precedence).
  if [ -n "${BOT_NAME}" ] && [ -z "$(directive_env_value user_nick)" ]; then
    validate_no_newline "IROFFER_BOT_NAME" "${BOT_NAME}"
    printf 'user_nick\t%s\n' "${BOT_NAME}" >> "${overrides_file}"
  fi
  if [ -n "${PORT_RANGE_OVERRIDE}" ]; then
    validate_port_range
    if [ -z "$(directive_env_value tcprangestart)" ]; then
      printf 'tcprangestart\t%s\n' "${PORT_RANGE_START}" >> "${overrides_file}"
    fi
    if [ -z "$(directive_env_value tcprangelimit)" ]; then
      printf 'tcprangelimit\t%s\n' "${PORT_RANGE_LIMIT}" >> "${overrides_file}"
    fi
  fi
  if [ -n "${HTTP_PORT_OVERRIDE}" ]; then
    validate_http_port
    if [ -z "$(directive_env_value http_port)" ]; then
      printf 'http_port\t%s\n' "${HTTP_PORT_OVERRIDE}" >> "${overrides_file}"
    fi
  fi

  for directive in "${SINGLE_DIRECTIVES[@]}"; do
    value="$(directive_env_value "${directive}")"
    [ -z "${value}" ] && continue
    validate_no_newline "$(directive_env_name "${directive}")" "${value}"
    if in_list "${directive}" "${BOOL_DIRECTIVES[@]}"; then
      case "${value}" in
        yes|no) ;;
        *)
          echo "Invalid $(directive_env_name "${directive}")='${value}'. Bool directives accept only 'yes' or 'no'." >&2
          exit 1
          ;;
      esac
    fi
    printf '%s\t%s\n' "${directive}" "${value}" >> "${overrides_file}"
  done

  for directive in "${MULTI_DIRECTIVES[@]}"; do
    value="$(directive_env_value "${directive}")"
    [ -z "${value}" ] && continue
    validate_no_newline "$(directive_env_name "${directive}")" "${value}"
    parts=()
    local remainder="${value}"
    while [ -n "${remainder}" ]; do
      if [[ "${remainder}" == *"${MULTI_SEP}"* ]]; then
        part="${remainder%%"${MULTI_SEP}"*}"
        remainder="${remainder#*"${MULTI_SEP}"}"
      else
        part="${remainder}"
        remainder=""
      fi
      [ -n "${part}" ] && parts+=("${part}")
    done
    if [ "${#parts[@]}" -eq 0 ]; then
      echo "$(directive_env_name "${directive}") is set but contains no non-empty value." >&2
      exit 1
    fi
    for part in "${parts[@]}"; do
      printf '%s\t%s\n' "${directive}" "${part}" >> "${overrides_file}"
    done
  done
}

# Build generated network blocks from IROFFER_NETWORK_COUNT / IROFFER_NETWORK_<n>_<DIRECTIVE>.
build_network_blocks() {
  local blocks_file="$1"
  : > "${blocks_file}"

  local count="${IROFFER_NETWORK_COUNT:-}"
  [ -z "${count}" ] && return 0

  if ! [[ "${count}" =~ ^[0-9]+$ ]] || [ "${count}" -lt 1 ]; then
    echo "Invalid IROFFER_NETWORK_COUNT '${count}'. Expected a positive integer." >&2
    exit 1
  fi

  local i name directive var value part line found
  local -a parts
  for ((i = 1; i <= count; i++)); do
    var="IROFFER_NETWORK_${i}_NAME"
    name="${!var:-}"
    if [ -z "${name}" ]; then
      echo "IROFFER_NETWORK_COUNT=${count} but ${var} is not set. Every network needs a NAME." >&2
      exit 1
    fi
    validate_no_newline "${var}" "${name}"
    if [[ "${name}" == *"{"* || "${name}" == *"}"* ]]; then
      echo "Invalid ${var}='${name}': network names must not contain braces." >&2
      exit 1
    fi

    {
      echo "network ${name}"
      echo "{"

      for directive in "${NETWORK_DIRECTIVES[@]}"; do
        var="$(printf 'IROFFER_NETWORK_%s_%s' "${i}" "$(printf '%s' "${directive}" | tr '[:lower:]' '[:upper:]')")"
        value="${!var:-}"
        [ -z "${value}" ] && continue
        validate_no_newline "${var}" "${value}"
        if [[ "${value}" == *"{"* || "${value}" == *"}"* ]]; then
          echo "Invalid ${var}: value must not contain braces (would break the network block)." >&2
          exit 1
        fi
        if in_list "${directive}" "${BOOL_DIRECTIVES[@]}"; then
          case "${value}" in
            yes|no) ;;
            *)
              echo "Invalid ${var}='${value}'. Bool directives accept only 'yes' or 'no'." >&2
              exit 1
              ;;
          esac
        fi
        if in_list "${directive}" "${NETWORK_MULTI_DIRECTIVES[@]}"; then
          # multi per network
          parts=()
          local remainder="${value}"
          while [ -n "${remainder}" ]; do
            if [[ "${remainder}" == *"${MULTI_SEP}"* ]]; then
              part="${remainder%%"${MULTI_SEP}"*}"
              remainder="${remainder#*"${MULTI_SEP}"}"
            else
              part="${remainder}"
              remainder=""
            fi
            [ -n "${part}" ] && parts+=("${part}")
          done
          for part in "${parts[@]}"; do
            echo "${directive} ${part}"
          done
        else
          echo "${directive} ${value}"
        fi
      done

      # Warn about unknown IROFFER_NETWORK_<i>_* variables.
      while IFS='=' read -r var line; do
        [ -z "${var}" ] && continue
        local suffix="${var#IROFFER_NETWORK_${i}_}"
        if [ "${suffix}" = "NAME" ]; then
          continue
        fi
        local lower
        lower="$(printf '%s' "${suffix}" | tr '[:upper:]' '[:lower:]')"
        found=0
        for directive in "${NETWORK_DIRECTIVES[@]}"; do
          if [ "${directive}" = "${lower}" ]; then
            found=1
            break
          fi
        done
        if [ "${found}" -eq 0 ]; then
          echo "Warning: ${var} is not a network-scoped directive and was ignored." >&2
        fi
      done < <(env | grep "^IROFFER_NETWORK_${i}_" || true)

      echo "}"
    } >> "${blocks_file}"
  done
}

# Rewrite config: strip old managed regions + superseded active directive lines
# (outside network blocks), prepend the overrides region (so network-scoped
# values land on the first network), and append generated network blocks.
apply_env_overrides() {
  local config_path="$1"
  local overrides_file="$2"
  local blocks_file="$3"

  local tmp_config
  tmp_config="$(mktemp "${config_path}.XXXXXX")"

  local regenerate_networks=0
  if [ -n "${IROFFER_NETWORK_COUNT:-}" ]; then
    regenerate_networks=1
  fi

  {
    if [ -s "${overrides_file}" ]; then
      echo "${OVERRIDES_BEGIN}"
      while IFS="$(printf '\t')" read -r directive value; do
        printf '%s %s\n' "${directive}" "${value}"
      done < "${overrides_file}"
      echo "${OVERRIDES_END}"
    fi

    awk \
      -v overrides_file="${overrides_file}" \
      -v ob="${OVERRIDES_BEGIN}" -v oe="${OVERRIDES_END}" \
      -v nb="${NETWORKS_BEGIN}" -v ne="${NETWORKS_END}" \
      -v regen="${regenerate_networks}" \
      '
      BEGIN {
        while ((getline line < overrides_file) > 0) {
          tab = index(line, "\t")
          if (tab > 0)
            overridden[substr(line, 1, tab - 1)] = 1
        }
        close(overrides_file)
        skip_region = 0
        delete_networks = (regen == 1)
        net_depth = 0        # brace nesting inside a network block
        net_pending = 0      # saw "network x" line, "{" not yet seen
        in_network_block = 0 # deleting an original network block (1=braced, 2=braceless)
        REM = "#removed-by-env: "
        REMNET = "#removed-by-env-net: "
      }
      {
        line = $0
        # strip managed regions from previous runs
        if (line == ob || line == nb) { skip_region = 1; next }
        if (line == oe || line == ne) { skip_region = 0; next }
        if (skip_region) next

        # restore lines commented out by a previous run when their env var
        # is no longer set (removing an env var restores the original value)
        if (index(line, REM) == 1) {
          rest = substr(line, length(REM) + 1)
          t = rest
          sub(/^[ \t]+/, "", t)
          first = t
          sub(/[ \t].*$/, "", first)
          if (!(first in overridden)) { print rest; next }
          print line; next
        }
        if (index(line, REMNET) == 1) {
          if (!delete_networks) { print substr(line, length(REMNET) + 1); next }
          print line; next
        }

        trimmed = line
        sub(/^[ \t]+/, "", trimmed)

        if (delete_networks) {
          if (in_network_block == 2) {
            # braceless so far: "network x" seen, "{" may still come
            if (trimmed ~ /^\{[ \t]*$/) {
              in_network_block = 1; net_depth = 1
              print REMNET line; next
            }
            if (trimmed ~ /^network([ \t]|$)/) {
              in_network_block = 0
              # fall through to handle this new network line below
            } else { print REMNET line; next }
          }
          if (in_network_block == 1) {
            if (trimmed ~ /^\{/) { net_depth++ }
            else if (trimmed ~ /^\}/) {
              net_depth--
              if (net_depth <= 0) { in_network_block = 0 }
            }
            print REMNET line; next
          }
          if (trimmed ~ /^network([ \t]|$)/) {
            if (index(trimmed, "{") > 0) { net_depth = 1; in_network_block = 1 }
            else { in_network_block = 2 }
            print REMNET line; next
          }
        }

        # track network block depth so we never strip directives inside blocks
        if (trimmed ~ /^network([ \t]|$)/ && in_network_block == 0) {
          if (index(trimmed, "{") > 0) { net_depth = 1 } else { net_pending = 1 }
          print line; next
        }
        if (net_pending) {
          if (trimmed ~ /^\{[ \t]*$/) { net_pending = 0; net_depth = 1; print line; next }
          # anything else (not blank/comment) means the block never opened
          if (trimmed !~ /^#/ && trimmed != "") { net_pending = 0 }
        }
        if (net_depth > 0) {
          if (trimmed ~ /^\{/) { net_depth++ }
          else if (trimmed ~ /^\}/) { net_depth-- }
          print line; next
        }

        # outside any block: comment out active lines for overridden
        # directives (restored automatically when the env var is removed)
        if (net_pending == 0) {
          first = trimmed
          sub(/[ \t].*$/, "", first)
          if (first in overridden && trimmed !~ /^#/) { print REM line; next }
        }
        print line
      }
      ' "${config_path}"

    if [ -s "${blocks_file}" ]; then
      echo "${NETWORKS_BEGIN}"
      cat "${blocks_file}"
      echo "${NETWORKS_END}"
    fi
  } > "${tmp_config}"

  cat "${tmp_config}" > "${config_path}"
  rm -f "${tmp_config}"
  chown "${APP_USER}": "${config_path}"
}

# Warn about IROFFER_* variables that map to no known directive (typos are
# otherwise silently ignored). Legacy aliases and network-block vars excluded.
warn_unknown_env_vars() {
  local var directive known
  while IFS='=' read -r var _; do
    [ -z "${var}" ] && continue
    case "${var}" in
      IROFFER_BOT_NAME|IROFFER_USER_ID|IROFFER_GROUP_ID) continue ;;
      IROFFER_NETWORK_COUNT) continue ;;
      IROFFER_NETWORK_[0-9]*) continue ;;
      IROFFER_*) ;;
      *) continue ;;
    esac
    directive="$(printf '%s' "${var#IROFFER_}" | tr '[:upper:]' '[:lower:]')"
    known=0
    if in_list "${directive}" "${SINGLE_DIRECTIVES[@]}" || in_list "${directive}" "${MULTI_DIRECTIVES[@]}"; then
      known=1
    fi
    if [ "${known}" -eq 0 ]; then
      echo "Warning: ${var} does not match any iroffer config directive and was ignored." >&2
    fi
  done < <(env | grep '^IROFFER_' || true)
}

apply_environment_to_config() {
  local config_path="${CONFIG_DIR}/${CONFIG_FILE_NAME}"

  if [ ! -f "${config_path}" ]; then
    return
  fi

  warn_unknown_env_vars

  OVERRIDES_TMP="$(mktemp)"
  BLOCKS_TMP="$(mktemp)"
  CONFIG_TMP_GLOB="${config_path}.??????"
  trap 'rm -f "${OVERRIDES_TMP}" "${BLOCKS_TMP}" ${CONFIG_TMP_GLOB}' EXIT
  local overrides_file blocks_file
  overrides_file="${OVERRIDES_TMP}"
  blocks_file="${BLOCKS_TMP}"

  build_overrides "${overrides_file}"
  build_network_blocks "${blocks_file}"

  apply_env_overrides "${config_path}" "${overrides_file}" "${blocks_file}"

  local applied
  applied=$(wc -l < "${overrides_file}")
  if [ "${applied}" -gt 0 ] || [ -s "${blocks_file}" ]; then
    echo "Applied ${applied} environment override(s) to ${config_path}."
    if [ -s "${blocks_file}" ]; then
      echo "Regenerated network block(s) from IROFFER_NETWORK_* (count=${IROFFER_NETWORK_COUNT})."
    fi
  fi

  # Warn if no active network/server config remains (bot cannot connect).
  if ! grep -Eq '^(network|server)([[:space:]]|$)' "${config_path}"; then
    echo "Warning: ${config_path} has no active 'network'/'server' directive." >&2
    echo "         Set IROFFER_NETWORK_COUNT + IROFFER_NETWORK_<n>_* or edit the config." >&2
    echo "         (Lines commented as '#removed-by-env:' can be restored manually.)" >&2
  fi

  rm -f "${overrides_file}" "${blocks_file}"
}

# Startup
if [[ -z ${1} ]]; then
# default
# prep
  init_config
  apply_environment_to_config
  run_iroffer_as_app_user -kns -w "${APP_HOME}/" "${CONFIG_DIR}/${CONFIG_FILE_NAME}"
else
# -?|-h|-v|-c
  case "$1" in
    /iroffer|./iroffer|iroffer)
      shift
      run_iroffer_as_app_user "$@"
      ;;
    *)
      exec "$@"
      ;;
  esac
fi
