package example

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/go-hclog"
	capabilityv1 "github.com/rancher/opni/pkg/apis/capability/v1"
	corev1 "github.com/rancher/opni/pkg/apis/core/v1"
	managementv1 "github.com/rancher/opni/pkg/apis/management/v1"
	"github.com/rancher/opni/pkg/logger"
	gatewayext "github.com/rancher/opni/pkg/plugins/apis/apiextensions/gateway"
	unaryext "github.com/rancher/opni/pkg/plugins/apis/apiextensions/gateway/unary"
	managementext "github.com/rancher/opni/pkg/plugins/apis/apiextensions/management"
	"github.com/rancher/opni/pkg/plugins/apis/capability"
	"github.com/rancher/opni/pkg/plugins/apis/system"
	"github.com/rancher/opni/pkg/plugins/meta"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExamplePlugin struct {
	UnsafeExampleAPIExtensionServer
	UnsafeExampleUnaryExtensionServer
	capabilityv1.UnsafeBackendServer
	system.UnimplementedSystemPluginClient
	Logger hclog.Logger
}

func (s *ExamplePlugin) Echo(_ context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{
		Message: req.Message,
	}, nil
}

func (s *ExamplePlugin) Hello(context.Context, *emptypb.Empty) (*EchoResponse, error) {
	return &EchoResponse{
		Message: "Hello World",
	}, nil
}

func (s *ExamplePlugin) UseManagementAPI(api managementv1.ManagementClient) {
	lg := s.Logger
	lg.Info("querying management API...")
	var list *managementv1.APIExtensionInfoList
	for {
		var err error
		list, err = api.APIExtensions(context.Background(), &emptypb.Empty{})
		if err != nil {
			log.Fatal(err)
		}
		if len(list.Items) == 0 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		break
	}
	for _, ext := range list.Items {
		lg.Info("found API extension service", "name", ext.ServiceDesc.GetName())
	}
}

func (s *ExamplePlugin) UseKeyValueStore(client system.KeyValueStoreClient) {
	kv := system.NewKVStoreClient[*EchoRequest](client)
	lg := s.Logger
	err := kv.Put(context.Background(), "foo", &EchoRequest{
		Message: "hello",
	})
	if err != nil {
		lg.Error("kv store error", "error", err)
	}

	value, err := kv.Get(context.Background(), "foo")
	if err != nil {
		lg.Error("kv store error", "error", err)
	}
	if value == nil {
		lg.Error("kv store error", "error", "value is nil")
	}
	lg.Info("successfully retrieved stored value", "message", value.Message)
}

func (s *ExamplePlugin) ConfigureRoutes(app *gin.Engine) {
	app.GET("/example", func(c *gin.Context) {
		s.Logger.Debug("handling /example")
		c.JSON(http.StatusOK, map[string]string{
			"message": "hello world",
		})
	})
}

func (s *ExamplePlugin) Info(context.Context, *emptypb.Empty) (*capabilityv1.InfoResponse, error) {
	return &capabilityv1.InfoResponse{
		CapabilityName: "test",
	}, nil
}

func (s *ExamplePlugin) CanInstall(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, nil
}

func (s *ExamplePlugin) Install(context.Context, *capabilityv1.InstallRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (s *ExamplePlugin) Uninstall(context.Context, *capabilityv1.UninstallRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Uninstall not implemented")
}

func (s *ExamplePlugin) UninstallStatus(context.Context, *corev1.Reference) (*corev1.TaskStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UninstallStatus not implemented")
}

func (s *ExamplePlugin) CancelUninstall(context.Context, *corev1.Reference) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelUninstall not implemented")
}

func (s *ExamplePlugin) InstallerTemplate(context.Context, *emptypb.Empty) (*capabilityv1.InstallerTemplateResponse, error) {
	return &capabilityv1.InstallerTemplateResponse{
		Template: `foo {{ arg "input" "Input" "+omitEmpty" "+default:default" "+format:--bar={{ value }}" }} ` +
			`{{ arg "toggle" "Toggle" "+omitEmpty" "+default:false" "+format:--reticulateSplines" }} ` +
			`{{ arg "select" "Select" "" "foo" "bar" "baz" "+omitEmpty" "+default:foo" "+format:--select={{ value }}" }}`,
	}, nil
}

func Scheme(ctx context.Context) meta.Scheme {
	scheme := meta.NewScheme()
	p := &ExamplePlugin{
		Logger: logger.NewForPlugin(),
	}
	scheme.Add(managementext.ManagementAPIExtensionPluginID,
		managementext.NewPlugin(&ExampleAPIExtension_ServiceDesc, p))
	scheme.Add(system.SystemPluginID, system.NewPlugin(p))
	scheme.Add(gatewayext.GatewayAPIExtensionPluginID,
		gatewayext.NewPlugin(p))
	scheme.Add(capability.CapabilityBackendPluginID, capability.NewPlugin(p))
	scheme.Add(unaryext.UnaryAPIExtensionPluginID, unaryext.NewPlugin(&ExampleUnaryExtension_ServiceDesc, p))
	return scheme
}
