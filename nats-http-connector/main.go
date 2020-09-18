package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/fission/keda-connectors/common"
)

type natsConnector struct {
	host          string
	connectordata common.ConnectorMetadata
	connection    *nats.Conn
	logger        *zap.Logger
}

func (conn natsConnector) consumeMessage() {

	headers := http.Header{
		"Topic":        {conn.connectordata.Topic},
		"RespTopic":    {conn.connectordata.ResponseTopic},
		"ErrorTopic":   {conn.connectordata.ErrorTopic},
		"Content-Type": {conn.connectordata.ContentType},
		"Source-Name":  {conn.connectordata.SourceName},
	}
	forever := make(chan bool)

	_, err := conn.connection.Subscribe(conn.connectordata.Topic, func(m *nats.Msg) {
		msg := string(m.Data)
		conn.logger.Info(msg)
		resp, err := common.HandleHTTPRequest(msg, headers, conn.connectordata, conn.logger)
		if err != nil {
			conn.errorHandler(err)
		} else {
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				conn.errorHandler(err)
			} else {
				if success := conn.responseHandler(string(body)); success {
				}
			}
		}
	})

	if err != nil {
		conn.logger.Fatal("error occurred while consuming message", zap.Error(err))
	}

	conn.logger.Info("NATs consumer up and running!...")
	<-forever
}

func (conn natsConnector) errorHandler(err error) {

	publishErr := conn.connection.Publish(conn.connectordata.ErrorTopic, []byte(err.Error()))

	if publishErr != nil {
		conn.logger.Error("failed to publish message to error topic",
			zap.Error(publishErr),
			zap.String("source", conn.connectordata.SourceName),
			zap.String("message", publishErr.Error()),
			zap.String("topic", conn.connectordata.ErrorTopic))
	}
}

func (conn natsConnector) responseHandler(response string) bool {

	if len(conn.connectordata.ResponseTopic) > 0 {

		publishErr := conn.connection.Publish(conn.connectordata.ResponseTopic, []byte(response))

		if publishErr != nil {
			conn.logger.Error("failed to publish response body from http request to topic",
				zap.Error(publishErr),
				zap.String("topic", conn.connectordata.ResponseTopic),
				zap.String("source", conn.connectordata.SourceName),
				zap.String("http endpoint", conn.connectordata.HTTPEndpoint),
			)
			return false
		}
	}
	return true
}

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}
	defer logger.Sync()

	connectordata, err := common.ParseConnectorMetadata()

	host := os.Getenv("HOST")
	if os.Getenv("INCLUDE_UNACKED") == "true" {
		logger.Fatal("only nats protocol host is supported")
	}
	if host == "" {
		logger.Fatal("received empty host field")
	}

	connection, err := nats.Connect(host)

	if err != nil {
		logger.Fatal("failed to establish connection with NATS", zap.Error(err))
	}
	defer connection.Close()

	conn := natsConnector{
		host:          host,
		connection:    connection,
		connectordata: connectordata,
		logger:        logger,
	}
	conn.consumeMessage()
}