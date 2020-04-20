FROM daocloud.io/ubuntu:trusty

ENV TZ=Asia/Shanghai

RUN apt-get update && apt-get install -y curl

RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

COPY sendxmail /usr/local/bin/sendxmail

RUN mkdir -p /etc/sendxmail && chmod +x /usr/local/bin/sendxmail

COPY cfg.json /etc/sendxmail/cfg.json
COPY template /etc/

ENTRYPOINT ["sendxmail", "-c", "/etc/sendxmail/cfg.json"]
