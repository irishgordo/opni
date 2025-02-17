package commands

import (
	"bytes"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	corev1 "github.com/rancher/opni/pkg/apis/core/v1"

	cliutil "github.com/rancher/opni/pkg/opni/util"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

func BuildCertsCmd() *cobra.Command {
	certsCmd := &cobra.Command{
		Use:   "certs",
		Short: "Manage certificates",
	}
	certsCmd.AddCommand(BuildCertsInfoCmd())
	certsCmd.AddCommand(BuildCertsPinCmd())
	ConfigureManagementCommand(certsCmd)
	return certsCmd
}

func pemEncodedChain(chain []*corev1.CertInfo) []byte {
	buf := new(bytes.Buffer)
	for _, c := range chain {
		if err := pem.Encode(buf, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}); err != nil {
			lg.Fatal(err)
		}
	}
	return buf.Bytes()
}

func BuildCertsInfoCmd() *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show certificate information",
		Run: func(cmd *cobra.Command, args []string) {
			t, err := mgmtClient.CertsInfo(cmd.Context(), &emptypb.Empty{})
			if err != nil {
				lg.Fatal(err)
			}
			switch outputFormat {
			case "table":
				fmt.Println(cliutil.RenderCertInfoChain(t.Chain))
			case "pem":
				fmt.Print(string(pemEncodedChain(t.Chain)))
			case "base64":
				fmt.Print(base64.StdEncoding.EncodeToString(pemEncodedChain(t.Chain)))
			default:
				lg.Fatal("unknown output format")
			}
		},
	}
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table|pem|base64)")
	return cmd
}

func BuildCertsPinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Print the fingerprint (pin) of the last certificate in the gateway's cert chain",
		Run: func(cmd *cobra.Command, args []string) {
			t, err := mgmtClient.CertsInfo(cmd.Context(), &emptypb.Empty{})
			if err != nil {
				lg.Fatal(err)
			}
			if len(t.Chain) == 0 {
				lg.Fatal("no certificates found")
			}
			pin := t.Chain[len(t.Chain)-1].Fingerprint
			fmt.Println(pin)
		},
	}
	return cmd
}
