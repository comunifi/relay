package config

import (
	"context"
	"log"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

type Config struct {
	RelayUrl             string `env:"RELAY_URL,required"`
	ChainName            string `env:"CHAIN_NAME,required"`
	RPCURL               string `env:"RPC_URL,required"`
	RPCWSURL             string `env:"RPC_WS_URL,required"`
	DBUser               string `env:"DB_USER,required"`
	DBPassword           string `env:"DB_PASSWORD,required"`
	DBName               string `env:"DB_NAME,required"`
	DBHost               string `env:"DB_HOST,required"`
	DBPort               string `env:"DB_PORT,required"`
	DBReaderHost         string `env:"DB_READER_HOST,required"`
	DBSecret             string `env:"DB_SECRET,required"`
	PinataBaseURL        string `env:"PINATA_BASE_URL"`
	PinataAPIKey         string `env:"PINATA_API_KEY"`
	PinataAPISecret      string `env:"PINATA_API_SECRET"`
	DiscordURL           string `env:"DISCORD_URL"`
	RelayPrivateKey      string `env:"RELAY_PRIVATE_KEY"`
	RelayInfoName        string `env:"RELAY_INFO_NAME"`
	RelayInfoDescription string `env:"RELAY_INFO_DESCRIPTION"`
	RelayInfoIcon        string `env:"RELAY_INFO_ICON"`
	AWSAccessKeyID       string `env:"AWS_ACCESS_KEY_ID"`
	AWSDefaultRegion     string `env:"AWS_DEFAULT_REGION"`
	AWSEndpointUrl       string `env:"AWS_ENDPOINT_URL"`
	AWSS3BucketName      string `env:"AWS_S3_BUCKET_NAME"`
	AWSSecretAccessKey   string `env:"AWS_SECRET_ACCESS_KEY"`
}

func New(ctx context.Context, envpath string) (*Config, error) {
	if envpath != "" {
		log.Default().Println("loading env from file: ", envpath)
		err := godotenv.Load(envpath)
		if err != nil {
			return nil, err
		}
	}

	cfg := &Config{}
	err := envconfig.Process(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}
