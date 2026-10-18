FROM debian:stable-slim AS builder

ARG DEBIAN_FRONTEND=noninteractive
# Pinned source: update URL and SHA256 together.
ARG IROFFER_URL="https://iroffer.net/iroffer-dinoex-snap.tar.gz"
ARG IROFFER_SHA256="83ef3aa28de2d9f959f4b9ceff3a2a3cb84f11870b6d3fbf67ba2790061d8d19"
ENV BUILD_ARGS="-curl -upnp -ruby"

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends \
	  ca-certificates \
	  curl \
	  gcc \
	  libc-dev \
	  libcurl4-openssl-dev \
	  libminiupnpc-dev \
	  libssl-dev \
	  make \
	  ruby \
	  ruby-dev \
	  tar; \
	rm -rf /var/lib/apt/lists/*

WORKDIR /tmp/src
RUN set -eux; \
	curl -fsSL "${IROFFER_URL}" -o /tmp/iroffer-dinoex.tar.gz; \
	echo "${IROFFER_SHA256}  /tmp/iroffer-dinoex.tar.gz" | sha256sum -c -; \
	tar -xzf /tmp/iroffer-dinoex.tar.gz --strip-components=1 -C /tmp/src; \
	rm -f /tmp/iroffer-dinoex.tar.gz

RUN set -eux; \
	chmod +x ./Configure; \
	./Configure ${BUILD_ARGS}; \
	make -j"$(nproc)"

# Stage outputs: compiled binary + default web/config assets.
RUN set -eux; \
	mkdir -p /out/extras/www; \
	cp -p ./iroffer /out/iroffer; \
	cp -p ./*.html /out/extras/www/; \
	cp -rp ./htdocs /out/extras/www/; \
	cp -p ./sample.config /out/extras/sample.config; \
	cp -p ./sample.config /out/extras/sample.customized.config; \
	chmod 600 /out/extras/sample.config /out/extras/sample.customized.config; \
	random_nick="mybot$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"; \
	sed -i -e "s|^user_nick mybotDCC$|user_nick ${random_nick}|" /out/extras/sample.customized.config; \
	sed -i -e "s|pidfile mybot.pid|pidfile /home/iroffer/config/mybot.pid|g" /out/extras/sample.customized.config; \
	sed -i -e "s|logfile mybot.log|logfile /home/iroffer/logs/mybot.log|g" /out/extras/sample.customized.config; \
	sed -i -e "s|statefile mybot.state|statefile /home/iroffer/config/mybot.state|g" /out/extras/sample.customized.config; \
	sed -i -e "s|xdcclistfile mybot.txt|xdcclistfile /home/iroffer/packlist.txt|g" /out/extras/sample.customized.config; \
	sed -i -e "s|^#filedir /home/me/files$|filedir /data|" /out/extras/sample.customized.config; \
	sed -i '/^# 1st Network, only IPv4$/, /^$/ s|^channel #dinoex -noannounce$|channel #nibl -noannounce|' /out/extras/sample.customized.config; \
	sed -i "/# 2nd Network/,/^$/d" /out/extras/sample.customized.config; \
	sed -i "/# 3st Network/,/^$/d" /out/extras/sample.customized.config; \
	sed -i "/#no_status_log/s/#//g" /out/extras/sample.customized.config


FROM debian:stable-slim

ARG DEBIAN_FRONTEND=noninteractive
ARG IROFFER_USER_ID=1000
ARG IROFFER_GROUP_ID=1000

# Every iroffer config directive is configurable via environment variable:
#   IROFFER_<DIRECTIVE_UPPERCASE>=value   (empty = keep iroffer.config value)
# Multi-value directives accept ";;"-separated values (e.g. IROFFER_FILEDIR="/data;;/data2").
# IROFFER_NETWORK_COUNT + IROFFER_NETWORK_<n>_<DIRECTIVE> generate network blocks.
# Values are applied to iroffer.config on every container start by entrypoint.sh.
# See .env.example / README.md for the full list with docs.
ENV IROFFER_BOT_NAME=mybot \
    PORT_RANGE=30000-31000
ENV \
    IROFFER_ADDDIR_EXCLUDE="" IROFFER_ADDDIR_MATCH="" IROFFER_ADDDIR_MIN_SIZE="" \
    IROFFER_ADMIN_JOB_DONE_FILE="" IROFFER_ADMIN_JOB_FILE="" IROFFER_ADMINHOST="" \
    IROFFER_ADMINLEVEL="" IROFFER_ADMINPASS="" IROFFER_ANNOUNCE_SEPERATOR="" \
    IROFFER_ANNOUNCE_SIZE="" IROFFER_ANNOUNCE_SUFFIX_COLOR="" IROFFER_ATFIND="" \
    IROFFER_AUTH_NAME="" IROFFER_AUTO_CRC_CHECK="" IROFFER_AUTO_DEFAULT_GROUP="" \
    IROFFER_AUTO_PATH_GROUP="" IROFFER_AUTOADD_COLOR="" IROFFER_AUTOADD_DELAY="" \
    IROFFER_AUTOADD_DIR="" IROFFER_AUTOADD_GROUP="" IROFFER_AUTOADD_GROUP_MATCH="" \
    IROFFER_AUTOADD_SORT="" IROFFER_AUTOADD_TIME="" IROFFER_AUTOADDANN="" \
    IROFFER_AUTOADDANN_MASK="" IROFFER_AUTOADDANN_SHORT="" IROFFER_AUTOCRC_EXCLUDE="" \
    IROFFER_AUTOIGNORE_EXCLUDE="" IROFFER_AUTOIGNORE_RATE="" IROFFER_AUTOIGNORE_THRESHOLD="" \
    IROFFER_AUTOSENDPACK="" IROFFER_BALANCED_QUEUE="" IROFFER_CHANNEL="" \
    IROFFER_CHANNEL_JOIN_RAW="" IROFFER_CHARSET="" IROFFER_CONNECTIONMETHOD="" \
    IROFFER_CREDITLINE="" IROFFER_DEBUG="" IROFFER_DIRECT_CONFIG_ACCESS="" \
    IROFFER_DIRECT_FILE_ACCESS="" IROFFER_DISABLEXDCCINFO="" IROFFER_DISK_QUOTA="" \
    IROFFER_DOS_TEXT_FILES="" IROFFER_DOWNLOAD_COMPLETED_MSG="" IROFFER_DOWNLOADHOST="" \
    IROFFER_DUMP_ALL="" IROFFER_ENABLE_NICK="" IROFFER_EXPIRE_LOGFILES="" \
    IROFFER_EXTEND_STATUS_LINE="" IROFFER_FILEDIR="" IROFFER_FILEREMOVE_MAX_PACKS="" \
    IROFFER_FISH_EXCLUDE_NICK="" IROFFER_FISH_ONLY="" IROFFER_FLOOD_PROTECTION_RATE="" \
    IROFFER_GETIP_NETWORK="" IROFFER_GETIPFROMSERVER="" IROFFER_GETIPFROMUPNP="" \
    IROFFER_GROUP_ADMIN="" IROFFER_GROUP_SEPERATOR="" IROFFER_GROUPSINCAPS="" \
    IROFFER_HADMINHOST="" IROFFER_HADMINLEVEL="" IROFFER_HADMINPASS="" \
    IROFFER_HEADLINE="" IROFFER_HIDE_LIST_INFO="" IROFFER_HIDE_LIST_STOP="" \
    IROFFER_HIDELOCKEDPACKS="" IROFFER_HIDEOS="" IROFFER_HOLDQUEUE="" \
    IROFFER_HTTP_ACCESS_LOG="" IROFFER_HTTP_ADMIN="" IROFFER_HTTP_ADMIN_DIR="" \
    IROFFER_HTTP_ALLOW="" IROFFER_HTTP_DATE="" IROFFER_HTTP_DENY="" \
    IROFFER_HTTP_DIR="" IROFFER_HTTP_FORBIDDEN="" IROFFER_HTTP_GEOIP="" \
    IROFFER_HTTP_INDEX="" IROFFER_HTTP_PORT="" IROFFER_HTTP_SEARCH="" \
    IROFFER_HTTP_VHOST="" IROFFER_IDLEQUEUESIZE="" IROFFER_IGNORE_DUPLICATE_IP="" \
    IROFFER_IGNOREDUPLICATEIP="" IROFFER_IGNOREUPLOADBANDWIDTH="" IROFFER_INCLUDE_SUBDIRS="" \
    IROFFER_LOCAL_VHOST="" IROFFER_LOG_EXCLUDE_HOST="" IROFFER_LOG_EXCLUDE_TEXT="" \
    IROFFER_LOGFILE="" IROFFER_LOGFILE_HTTPD="" IROFFER_LOGFILE_MESSAGES="" \
    IROFFER_LOGFILE_NOTICES="" IROFFER_LOGIN_NAME="" IROFFER_LOGINNAME="" \
    IROFFER_LOGMESSAGES="" IROFFER_LOGNOTICES="" IROFFER_LOGROTATE="" \
    IROFFER_LOGSTATS="" IROFFER_LOWBDWTH="" IROFFER_MAX_FIND="" \
    IROFFER_MAX_UPLOADS="" IROFFER_MAX_UPSPEED="" IROFFER_MAXIDLEQUEUEDPERPERSON="" \
    IROFFER_MAXQUEUEDITEMSPERPERSON="" IROFFER_MAXTRANSFERSPERPERSON="" IROFFER_MD5SUM_EXCLUDE="" \
    IROFFER_MIME_TYPE="" IROFFER_MIRC_DCC64="" IROFFER_MONITOR_FILES="" \
    IROFFER_NEED_LEVEL="" IROFFER_NEED_VOICE="" \
    IROFFER_NEW_TRIGGER="" IROFFER_NICKSERV_PASS="" IROFFER_NO_AUTO_REHASH="" \
    IROFFER_NO_DUPLICATE_FILENAMES="" IROFFER_NO_FIND_TRIGGER="" IROFFER_NO_MINSPEED_ON_FREE="" \
    IROFFER_NO_NATURAL_SORT="" IROFFER_NO_STATUS_CHAT="" IROFFER_NO_STATUS_LOG="" \
    IROFFER_NOANNOUNCE="" IROFFER_NOAUTOREJOIN="" IROFFER_NOCRC32="" \
    IROFFER_NODOWNLOADHOST="" IROFFER_NODUPLICATEFILES="" IROFFER_NOMD5SUM="" \
    IROFFER_NOTIFYTIME="" IROFFER_OFFLINE="" IROFFER_OLD_STATEFILE="" \
    IROFFER_OVERALLMAXSPEED="" IROFFER_OVERALLMAXSPEEDDAYDAYS="" IROFFER_OVERALLMAXSPEEDDAYSPEED="" \
    IROFFER_OVERALLMAXSPEEDDAYTIME="" IROFFER_OWNER_NICK="" IROFFER_PASSIVE_DCC="" \
    IROFFER_PASSIVE_DCC_CHAT="" IROFFER_PERIODICMSG="" IROFFER_PIDFILE="" \
    IROFFER_PLAINTEXT="" IROFFER_PRIVMSG_ENCRYPT="" IROFFER_PRIVMSG_FISH="" \
    IROFFER_PROXYINFO="" IROFFER_PUNISHSLOWUSERS="" IROFFER_QUEUESIZE="" \
    IROFFER_QUIETMODE="" IROFFER_QUIT_MSG="" IROFFER_RECONNECT_DELAY="" \
    IROFFER_REMINDER_SEND_RETRY="" IROFFER_REMOVE_DEAD_USERS="" IROFFER_REMOVELOSTFILES="" \
    IROFFER_REQUEUE_SENDS="" IROFFER_RESPONDTOCHANNELLIST="" IROFFER_RESPONDTOCHANNELLISTMSG="" \
    IROFFER_RESPONDTOCHANNELXDCC="" IROFFER_RESTRICTLIST="" IROFFER_RESTRICTPRIVLIST="" \
    IROFFER_RESTRICTPRIVLISTFULL="" IROFFER_RESTRICTPRIVLISTMAIN="" IROFFER_RESTRICTPRIVLISTMSG="" \
    IROFFER_RESTRICTSEND="" IROFFER_RESTRICTSEND_DELAY="" IROFFER_RESTRICTSEND_TIMEOUT="" \
    IROFFER_RESTRICTSEND_WARNING="" IROFFER_RUBY_SCRIPT="" IROFFER_SEND_BATCH="" \
    IROFFER_SEND_LISTFILE="" IROFFER_SEND_STATEFILE="" IROFFER_SEND_STATEFILE_MINUTE="" \
    IROFFER_SERVER="" IROFFER_SERVER_CONNECT_TIMEOUT="" IROFFER_SERVER_CONNECTED_RAW="" \
    IROFFER_SERVER_JOIN_RAW="" IROFFER_SERVER_SEND_MAX="" IROFFER_SERVER_SEND_RATE="" \
    IROFFER_SHOW_DATE_ADDED="" IROFFER_SHOW_GROUP_OF_PACK="" IROFFER_SHOW_LIST_ALL="" \
    IROFFER_SLOTSMAX="" IROFFER_SLOW_PRIVMSG="" IROFFER_SMALLFILEBYPASS="" \
    IROFFER_SPACES_IN_FILENAMES="" IROFFER_START_OF_MONTH="" IROFFER_STATEFILE="" \
    IROFFER_STATUS_TIME_DCC_CHAT="" IROFFER_SUBDIRS_DELAYED="" IROFFER_TCP_BUFFER_SIZE="" \
    IROFFER_TCP_NODELAY="" IROFFER_TCPRANGELIMIT="" IROFFER_TCPRANGESTART="" \
    IROFFER_TELNET_ALLOW="" IROFFER_TELNET_DENY="" IROFFER_TELNET_PORT="" \
    IROFFER_TELNET_VHOST="" IROFFER_TIMESTAMPCONSOLE="" IROFFER_TRANSFERLIMITS="" \
    IROFFER_TRANSFERMAXSPEED="" IROFFER_TRANSFERMINSPEED="" IROFFER_TRASHCAN_DIR="" \
    IROFFER_UNLIMITEDHOST="" IROFFER_UPLOADDIR="" IROFFER_UPLOADHOST="" \
    IROFFER_UPLOADMAXSIZE="" IROFFER_UPLOADMINSPACE="" IROFFER_UPNP_ROUTER="" \
    IROFFER_USENATIP="" IROFFER_USER_MODES="" IROFFER_USER_NICK="" \
    IROFFER_USER_REALNAME="" IROFFER_VERBOSE_CRC32="" IROFFER_WAITAFTERJOIN="" \
    IROFFER_WEBLIST_INFO="" IROFFER_XDCC_ALLOW="" IROFFER_XDCC_DENY="" \
    IROFFER_XDCCLIST_BY_PRIVMSG="" IROFFER_XDCCLIST_GROUPONLY="" IROFFER_XDCCLISTFILE="" \
    IROFFER_XDCCLISTFILERAW="" IROFFER_XDCCREMOVEFILE="" IROFFER_XDCCXMLFILE="" \
    IROFFER_NETWORK_COUNT=""

LABEL name="iroffer" \
	  description="iroffer-dinoex XDCC bot with curl, Ruby, and UPnP support"

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends \
	  ca-certificates \
	  libcurl4 \
	  libminiupnpc18 \
	  ruby; \
	rm -rf /var/lib/apt/lists/*; \
	groupadd --gid "${IROFFER_GROUP_ID}" --system iroffer; \
	useradd \
	  --uid "${IROFFER_USER_ID}" \
	  --gid "${IROFFER_GROUP_ID}" \
	  --system \
	  --create-home \
	  --home-dir /home/iroffer \
	  --shell /usr/sbin/nologin \
	  iroffer; \
	mkdir -p /extras /home/iroffer/config /home/iroffer/data /home/iroffer/logs

COPY --from=builder /out/iroffer /iroffer
COPY --from=builder /out/extras /extras
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN set -eux; \
	chmod 0755 /usr/local/bin/entrypoint.sh /iroffer; \
	ln -sf /usr/local/bin/entrypoint.sh /entrypoint.sh; \
	chown -R iroffer:iroffer /home/iroffer /extras

WORKDIR /home/iroffer
VOLUME ["/home/iroffer"]
EXPOSE ${PORT_RANGE}

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
