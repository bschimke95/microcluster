package trust

import (
	"crypto/x509"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/canonical/lxd/shared"
	"github.com/canonical/lxd/shared/api"
	"github.com/canonical/lxd/shared/logger"
	"github.com/google/renameio"
	"gopkg.in/yaml.v3"

	"github.com/canonical/microcluster/v3/client"
	internalClient "github.com/canonical/microcluster/v3/internal/rest/client"
	"github.com/canonical/microcluster/v3/rest/types"
)

// Remotes is a convenient alias as we will often deal with groups of yaml files.
type Remotes struct {
	data     map[string]Remote
	updateMu sync.RWMutex
}

// Remote represents a yaml file with credentials to be read by the daemon.
type Remote struct {
	Location    `yaml:",inline"`
	Certificate types.X509Certificate `yaml:"certificate"`
}

// Location represents configurable identifying information about a remote.
type Location struct {
	Name    string         `yaml:"name"`
	Address types.AddrPort `yaml:"address"`
}

// disallowedFileNameSubcontents contains the list of disallowed substrings in remote names.
var disallowedFilenameSubcontents = []string{"..", "/", "\\"}

// validateRemoteName checks if the remote name contains any disallowed substrings.
func validateRemoteName(name string) error {
	for _, disallowed := range disallowedFilenameSubcontents {
		if strings.Contains(name, disallowed) {
			return fmt.Errorf("Invalid remote name %q. Contains illegal subcontent %q", name, disallowed)
		}
	}

	return nil
}

// remoteYamlPath returns the path to the remote's YAML file. The path is checked to ensure it is within the given directory.
func remoteYamlPath(dir, name string) (string, error) {
	path, err := filepath.Abs(filepath.Join(dir, name+".yaml"))
	if err != nil {
		return "", fmt.Errorf("Failed to get absolute path to %q yaml: %w", name, err)
	}

	if !strings.HasPrefix(path, dir) {
		return "", fmt.Errorf("Invalid path to %q yaml", name)
	}

	return path, nil
}

// Load reads any yaml files in the given directory and parses them into a set of Remotes.
func (r *Remotes) Load(dir string) error {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("Unable to read trust directory: %q: %w", dir, err)
	}

	remoteData := map[string]Remote{}
	for _, file := range files {
		fileName := file.Name()
		if file.IsDir() || !strings.HasSuffix(fileName, ".yaml") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, fileName))
		if err != nil {
			return fmt.Errorf("Unable to read file %q: %w", fileName, err)
		}

		remote := &Remote{}
		err = yaml.Unmarshal(content, remote)
		if err != nil {
			return fmt.Errorf("Unable to parse yaml for %q: %w", fileName, err)
		}

		if remote.Certificate.Certificate == nil {
			return fmt.Errorf("Failed to parse local record %q. Found empty certificate", remote.Name)
		}

		remoteData[remote.Name] = *remote
	}

	// If the refreshed truststore data is empty, and we already had data in the truststore,
	// abort the refresh because an initialized system should always have truststore entries.
	if len(remoteData) == 0 && len(r.data) != 0 {
		logger.Warn("Failed to parse new remotes from truststore")

		return nil
	}

	r.data = remoteData

	return nil
}

// Add adds a new local cluster member record for the remotes.
func (r *Remotes) Add(dir string, remotes ...Remote) error {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	for _, remote := range remotes {
		if remote.Certificate.Certificate == nil {
			return fmt.Errorf("Failed to parse local record %q. Found empty certificate", remote.Name)
		}

		err := validateRemoteName(remote.Name)
		if err != nil {
			return err
		}

		_, ok := r.data[remote.Name]
		if ok {
			return fmt.Errorf("A remote with name %q already exists", remote.Name)
		}

		bytes, err := yaml.Marshal(remote)
		if err != nil {
			return fmt.Errorf("Failed to parse remote %q to yaml: %w", remote.Name, err)
		}

		path, err := remoteYamlPath(dir, remote.Name)
		if err != nil {
			return err
		}

		_, err = os.Stat(path)
		if err == nil {
			return fmt.Errorf("Remote at %q already exists", path)
		}

		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Failed to check remote path %q: %w", path, err)
		}

		err = renameio.WriteFile(path, bytes, 0644)
		if err != nil {
			return fmt.Errorf("Failed to write %q: %w", path, err)
		}

		// Add the remote manually so we can use it right away without waiting for inotify.
		r.data[remote.Name] = remote
	}

	return nil
}

// Replace replaces the in-memory and locally stored remotes with the given list from the database.
func (r *Remotes) Replace(dir string, newRemotes ...types.ClusterMember) error {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	if len(newRemotes) == 0 {
		return fmt.Errorf("Received empty remotes")
	}

	remoteData := map[string]Remote{}
	for _, remote := range newRemotes {
		newRemote := Remote{
			Location:    Location{Name: remote.Name, Address: remote.Address},
			Certificate: remote.Certificate,
		}

		if remote.Certificate.Certificate == nil {
			return fmt.Errorf("Failed to parse local record %q. Found empty certificate", remote.Name)
		}

		bytes, err := yaml.Marshal(newRemote)
		if err != nil {
			return fmt.Errorf("Failed to parse remote %q to yaml: %w", remote.Name, err)
		}

		remotePath, err := remoteYamlPath(dir, remote.Name)
		if err != nil {
			return err
		}

		err = renameio.WriteFile(remotePath, bytes, 0644)
		if err != nil {
			return fmt.Errorf("Failed to write %q: %w", remotePath, err)
		}

		remoteData[remote.Name] = newRemote
	}

	allEntries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Remove any outdated entries.
	for _, entry := range allEntries {
		name, found := strings.CutSuffix(entry.Name(), ".yaml")
		if !found {
			continue
		}

		_, ok := remoteData[name]

		if !ok {
			remotePath, err := remoteYamlPath(dir, name)
			if err != nil {
				return err
			}

			err = os.Remove(remotePath)
			if err != nil {
				return err
			}
		}
	}

	if len(remoteData) == 0 {
		return fmt.Errorf("Failed to parse new remotes")
	}

	r.data = remoteData

	return nil
}

// SelectRandom returns a random remote.
func (r *Remotes) SelectRandom() *Remote {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	allRemotes := make([]Remote, 0, len(r.data))
	for _, r := range r.data {
		allRemotes = append(allRemotes, r)
	}

	return &allRemotes[rand.Intn(len(allRemotes))]
}

// Addresses returns just the host:port addresses of the remotes.
func (r *Remotes) Addresses() map[string]types.AddrPort {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	addrs := map[string]types.AddrPort{}
	for _, remote := range r.data {
		addrs[remote.Name] = remote.Address
	}

	return addrs
}

// Cluster returns a set of clients for every remote, which can be concurrently queried.
func (r *Remotes) Cluster(isNotification bool, serverCert *shared.CertInfo, publicKey *x509.Certificate) (client.Cluster, error) {
	cluster := make(client.Cluster, 0, r.Count()-1)
	for _, addr := range r.Addresses() {
		url := api.NewURL().Scheme("https").Host(addr.String())
		c, err := internalClient.New(*url, serverCert, publicKey, isNotification)
		if err != nil {
			return nil, err
		}

		cluster = append(cluster, client.Client{Client: *c})
	}

	return cluster, nil
}

// RemoteByAddress returns a Remote matching the given host address (or nil if none are found).
func (r *Remotes) RemoteByAddress(addrPort types.AddrPort) *Remote {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	for _, remote := range r.data {
		if remote.Address.String() == addrPort.String() {
			return &remote
		}
	}

	return nil
}

// RemoteByCertificateFingerprint returns a remote whose certificate fingerprint matches the provided fingerprint.
func (r *Remotes) RemoteByCertificateFingerprint(fingerprint string) *Remote {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	for _, remote := range r.data {
		if fingerprint == shared.CertFingerprint(remote.Certificate.Certificate) {
			return &remote
		}
	}

	return nil
}

// Certificates returns a map of remotes certificates by fingerprint.
func (r *Remotes) Certificates() map[string]types.X509Certificate {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	certMap := map[string]types.X509Certificate{}
	for _, remote := range r.data {
		certMap[shared.CertFingerprint(remote.Certificate.Certificate)] = remote.Certificate
	}

	return certMap
}

// CertificatesNative returns the Certificates map with values as native x509.Certificate type.
func (r *Remotes) CertificatesNative() map[string]x509.Certificate {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	certMap := map[string]x509.Certificate{}
	for _, remote := range r.data {
		certMap[shared.CertFingerprint(remote.Certificate.Certificate)] = *remote.Certificate.Certificate
	}

	return certMap
}

// Count returns the number of remotes.
func (r *Remotes) Count() int {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	return len(r.data)
}

// RemotesByName returns a copy of the list of peers, keyed by each system's name.
func (r *Remotes) RemotesByName() map[string]Remote {
	r.updateMu.RLock()
	defer r.updateMu.RUnlock()

	remoteData := make(map[string]Remote, len(r.data))
	for name, data := range r.data {
		remoteData[name] = data
	}

	return remoteData
}

// URL returns the parsed URL of the Remote.
func (r *Remote) URL() api.URL {
	return *api.NewURL().Scheme("https").Host(r.Address.String())
}
