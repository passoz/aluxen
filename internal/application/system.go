package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
)

// Auxiliares compartilhados: configurações, contagens e resolução da URL pública.

// DefaultProductionURL é o endereço usado quando nada mais está configurado.
const DefaultProductionURL = "https://aluxen.evolucsia.com"

// Counts são as contagens exibidas em /api/info.
type Counts struct {
	Tenants    int
	Properties int
	Leases     int
	Receipts   int
}

// SystemCounts devolve as contagens de cadastros.
func SystemCounts(ctx context.Context, deps Deps) (Counts, error) {
	tenants, err := deps.Repos.Tenants.Count(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("contar locatários: %w", err)
	}
	properties, err := deps.Repos.Properties.Count(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("contar imóveis: %w", err)
	}
	leases, err := deps.Repos.Leases.Count(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("contar contratos: %w", err)
	}
	receipts, err := deps.Repos.Receipts.Count(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("contar pagamentos: %w", err)
	}
	return Counts{Tenants: tenants, Properties: properties, Leases: leases, Receipts: receipts}, nil
}

// SettingValue devolve o valor da configuração, ou string vazia quando ausente.
func SettingValue(ctx context.Context, deps Deps, key string) (string, error) {
	value, err := deps.Repos.Settings.Get(ctx, key)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("ler configuração %s: %w", key, err)
	}
	return value, nil
}

// Settings devolve todas as configurações como mapa.
func Settings(ctx context.Context, deps Deps) (map[string]string, error) {
	values, err := deps.Repos.Settings.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ler configurações: %w", err)
	}
	return values, nil
}

// SaveSetting grava uma configuração.
func SaveSetting(ctx context.Context, deps Deps, key, value string) error {
	if err := deps.Repos.Settings.Set(ctx, key, value); err != nil {
		return fmt.Errorf("gravar configuração %s: %w", key, err)
	}
	return nil
}

// ResolvePublicBaseURL resolve a base pública dos links de validação e dos QR
// codes, na mesma ordem de prioridade do app atual: configuração `app_url`,
// `APP_URL`, `BASE_URL`, host da requisição (quando não é localhost), IP da rede
// local e, por fim, o endereço de produção padrão.
func ResolvePublicBaseURL(ctx context.Context, deps Deps, requestHost string) string {
	if configured, err := SettingValue(ctx, deps, domain.SettingAppURL); err == nil && strings.TrimSpace(configured) != "" {
		return strings.TrimRight(strings.TrimSpace(configured), "/")
	}
	if configured := strings.TrimSpace(deps.Opts.PublicBaseURL); configured != "" {
		return strings.TrimRight(configured, "/")
	}
	if host := strings.TrimSpace(requestHost); host != "" && !isLocalHost(host) {
		scheme := "http"
		if strings.Contains(host, "evolucsia.com") {
			scheme = "https"
		}
		return scheme + "://" + host
	}
	if address := localNetworkAddress(); address != "" {
		return "http://" + address
	}
	return DefaultProductionURL
}

func isLocalHost(host string) bool {
	name := host
	if index := strings.Index(name, ":"); index >= 0 {
		name = name[:index]
	}
	switch name {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	default:
		return false
	}
}

// localNetworkAddress devolve o primeiro endereço IPv4 privado da máquina,
// ignorando interfaces de container (docker, bridge, veth, virbr, lxc).
func localNetworkAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if hasContainerPrefix(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			if ipv4 := ipNet.IP.To4(); ipv4 != nil {
				return ipv4.String()
			}
		}
	}
	return ""
}

func hasContainerPrefix(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "lxc"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
