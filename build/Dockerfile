FROM alpine:3.19
LABEL  MAINTAINER=fengxiaoqiang@dooffe.com
ENV TZ=PRC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

RUN apk add --no-cache shadow
#RUN useradd -m app
#USER app

RUN mkdir mars
WORKDIR /mars
ENV CFG=""
ENV PARAMS=""
ENV SEQ=""
ADD mars-amd64-upx /mars/mars-amd64-upx
CMD echo $CFG >/mars/.env && $SEQ >/mars/seq.conf && /mars/mars-amd64-upx $PARAMS
