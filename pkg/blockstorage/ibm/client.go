package ibm

import (
	"context"
	"strings"

	"github.com/BurntSushi/toml"
	ibmcfg "github.com/IBM/ibmcloud-storage-volume-lib/config"
	ibmprov "github.com/IBM/ibmcloud-storage-volume-lib/lib/provider"
	ibmprovutils "github.com/IBM/ibmcloud-storage-volume-lib/provider/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kanisterio/kanister/pkg/kube"
)

// IBM Cloud environment variable names
const (
	IBMK8sSecretName = "storage-secret-store"
	IBMK8sSecretData = "slclient.toml"
	IBMK8sSecretNS   = "kube-system"
	LibDefCfgEnv     = "SECRET_CONFIG_PATH"
)

var (
	blueMixCfg = ibmcfg.BluemixConfig{
		IamURL:          "https://iam.bluemix.net",
		IamClientID:     "bx",
		IamClientSecret: "bx",
		IamAPIKey:       "free",
		RefreshToken:    "",
	}

	softLayerCfg = ibmcfg.SoftlayerConfig{
		SoftlayerBlockEnabled:        true,
		SoftlayerBlockProviderName:   "SOFTLAYER-BLOCK",
		SoftlayerFileEnabled:         false,
		SoftlayerFileProviderName:    "SOFTLAYER-FILE",
		SoftlayerUsername:            "",
		SoftlayerAPIKey:              "",
		SoftlayerEndpointURL:         "https://api.softlayer.com/rest/v3",
		SoftlayerIMSEndpointURL:      "https://api.softlayer.com/mobile/v3",
		SoftlayerDataCenter:          "sjc03",
		SoftlayerTimeout:             "20s",
		SoftlayerVolProvisionTimeout: "10m",
		SoftlayerRetryInterval:       "5s",
	}
)

//client is a wrapper for Library client
type client struct {
	Service ibmprov.Session
	SLCfg   ibmcfg.SoftlayerConfig
}

//newClient returns a Client struct
func newClient(ctx context.Context, args map[string]string) (*client, error) {

	zaplog, _ := zap.NewProduction()
	defer zaplog.Sync() // nolint: errcheck

	cfg, err := findDefaultConfig(ctx, args, zaplog)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get IBM client config")
	}

	provReg, err := ibmprovutils.InitProviders(cfg, zaplog)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to Init IBM providers")
	}

	session, _, err := ibmprovutils.OpenProviderSession(cfg, provReg, cfg.Softlayer.SoftlayerBlockProviderName, zaplog)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create Open session for IBM provider. %s", cfg.Softlayer.SoftlayerBlockProviderName)
	}

	return &client{
		Service: session,
		SLCfg:   *cfg.Softlayer,
	}, nil
}

func findDefaultConfig(ctx context.Context, args map[string]string, zaplog *zap.Logger) (*ibmcfg.Config, error) {
	// Checking if an api key is provided via args
	// If it present will use api value and default Softlayer config
	if apik, ok := args[APIKeyArgName]; ok {
		blueMixCfg.IamAPIKey = strings.Replace(apik, "\"", "", 2)
		return &ibmcfg.Config{
			Softlayer: &softLayerCfg,
			Gen2:      &ibmcfg.Gen2Config{},
			Bluemix:   &blueMixCfg,
		}, nil
	}
	// Cheking if IBM store secret is present
	ibmCfg, err := getDefIBMStoreSecret(ctx, args)
	if err != nil {
		log.WithError(err).Info("Could not get IBM default store secret")
	} else {
		return ibmCfg, nil
	}
	// Final attemp to get Config, by using default lib code path
	defPath := ibmcfg.GetConfPath()
	return ibmcfg.ReadConfig(defPath, zaplog)
}

func getDefIBMStoreSecret(ctx context.Context, args map[string]string) (*ibmcfg.Config, error) {
	// Let's check if we are running in k8s and special IBM storage secret is present
	k8scli, err := kube.NewClient()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to created k8s client.")
	}
	secretNam := IBMK8sSecretName
	secretNS := IBMK8sSecretNS

	if sn, ok := args[CfgSecretNameArgName]; ok {
		secretNam = sn
	}

	if sns, ok := args[CfgSecretNameSpaceArgName]; ok {
		secretNS = sns
	}

	storeSecret, err := k8scli.CoreV1().Secrets(secretNS).Get(secretNam, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read Default IBM storage secret.")
	}
	retConfig := ibmcfg.Config{Softlayer: &softLayerCfg}
	_, err = toml.Decode(string(storeSecret.Data[IBMK8sSecretData]), &retConfig)
	return &retConfig, err
}
