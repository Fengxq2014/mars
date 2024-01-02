FROM alpine:3.19
LABEL  MAINTAINER=fengxiaoqiang@dooffe.com
ENV TZ=PRC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

RUN apk add --no-cache shadow
#RUN useradd -m app
#USER app

WORKDIR /mars
RUN mkdir mars
ENV CFG=""
ENV PARAMS=""
ADD mars-amd64-upx /mars/
CMD echo $CFG > .env && ./mars-amd64-upx $PARAMS
