package gitlabstub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/api"
)

// APIResponses abstracts the stubbed responses for gitlabstub.
// The collection of responses is a map on the following format:
//
//	"domain": {
//		"prefix": {
//			ProjectID:     0,
//			AccessControl: false,
//			HTTPS:         false,
//			PathOnDisk:    "base/path/for/the/public.zip/file",
//		},
//	}
//
// For Example:
//
//	"group.gitlab-example.com": {
//		"/project1": {
//			ProjectID: 1000,
//			AccessControl: true,
//			HTTPS: true,
//			PathOnDisk: "group/project",
//		},
//		"/project2": {
//			ProjectID: 1001,
//			AccessControl: true,
//			HTTPS: true,
//			PathOnDisk: "group/project",
//		},
//	}
type APIResponses map[string]Responses

type Responses map[string]Response

// A Response is the struct responsible for creating a project LookupPath.
type Response struct {
	projectID     int
	accessControl bool
	httpsOnly     bool
	pathOnDisk    string // base directory is gitlab-pages/shared/pages
	uniqueHost    string
	rootDirectory string
	primaryDomain string
}

func (responses Responses) virtualDomain(host, wd string) api.VirtualDomain {
	vd := api.VirtualDomain{
		Certificate:       "",
		Key:               "",
		ClientCertificate: "",
		RootNamespaceID:   defaultRootNamespaceID,
		LookupPaths:       responses.lookupPaths(wd),
	}
	if found, ok := tlsSettings[host]; ok {
		vd.Certificate = found.Certificate
		vd.Key = found.Key
		vd.ClientCertificate = found.ClientCertificate
	}
	return vd
}

func (responses Responses) lookupPaths(wd string) []api.LookupPath {
	lookupPaths := make([]api.LookupPath, 0, len(responses))

	for prefix, response := range responses {
		lookupPaths = append(lookupPaths, response.lookupPath(prefix, wd))
	}

	return lookupPaths
}

func (response Response) lookupPath(prefix, wd string) api.LookupPath {
	sourcePath := fmt.Sprintf("file://%s/%s/public.zip", wd, response.pathOnDisk)
	sum := sha256.Sum256([]byte(sourcePath))
	sha := hex.EncodeToString(sum[:])

	return api.LookupPath{
		Prefix:          prefix,
		ProjectID:       response.projectID,
		AccessControl:   response.accessControl,
		HTTPSOnly:       response.httpsOnly,
		UniqueHost:      response.uniqueHost,
		RootDirectory:   response.rootDirectory,
		PrimaryDomain:   response.primaryDomain,
		RootNamespaceID: defaultRootNamespaceID,
		Source: api.Source{
			Type:   "zip",
			Path:   sourcePath,
			SHA256: sha,
		},
	}
}

var tlsSettings = map[string]api.VirtualDomain{
	"mtls.gitlab-example.com": {
		Certificate:       "-----BEGIN CERTIFICATE-----\nMIIDZDCCAkygAwIBAgIRAOtN9/zy+gFjdsgpKq3QRdQwDQYJKoZIhvcNAQELBQAw\nMzEUMBIGA1UEChMLTG9nIENvdXJpZXIxGzAZBgNVBAMTEmdpdGxhYi1leGFtcGxl\nLmNvbTAgFw0xODAzMjMxODMwMDZaGA8yMTE4MDIyNzE4MzAwNlowMzEUMBIGA1UE\nChMLTG9nIENvdXJpZXIxGzAZBgNVBAMTEmdpdGxhYi1leGFtcGxlLmNvbTCCASIw\nDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKULsxpnazXX5RsVzayrAQB+lWwr\nWef5L5eDhSsIsBLbelYp5YB4TmVRt5x7bWKOOJSBsOfwHZHKJXdu+uuX2RenZlhk\n3Qpq9XGaPZjYm/NHi8gBHPAtz5sG5VaKNvkfTzRGnO9CWA9TM1XtYiOBq94dO+H3\nc+5jP5Yw+mJ+hA+i2058zF8nRlUHArEno2ofrHwE0LMZ11VskpXtWnVfs3voLs8p\nr76KXPBFkMJR4qkWrMDF5Y5MbsQ0zisn6KXrTyV0S4MQh4vSyPdFHnEzvJ07rm5x\n4RTWrjgQeQ2DjZjQvRmaDzlVBK9kaMkJ1Si3agK+gpji6d6WZ/Mb2el1GK8CAwEA\nAaNxMG8wDgYDVR0PAQH/BAQDAgKkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1Ud\nEwEB/wQFMAMBAf8wNwYDVR0RBDAwLoIUKi5naXRsYWItZXhhbXBsZS5jb22HBH8A\nAAGHEAAAAAAAAAAAAAAAAAAAAAEwDQYJKoZIhvcNAQELBQADggEBAJ0NM8apK0xI\nYxMstP/dCQXtR0wyREGSD/eOpeY3bWlqCbpRgMFUGjQlrsEozcPZOCSCKX5p+tym\n7GsnYtXkwbsuURoSz+5IlhRPVHcUlUeGRdv3/gCd8fDXiigALCsB6GrkMG5cUfh+\nx5p52AC3eQdWTDoxNou+2gzwkAl8iJc13Ykusst0YUqcsXKqTuei2quxFv0pEBSO\np8wEixoicLFNqPnIDmgx5894DAn0bccNXgRWtq8lLbdhGUlBbpatevvFMgNvFUbe\neeGb9D0EfpxmzxUl+L0xZtfg3f7cu5AgLG8tb6l4AK6NPVuXN8DmUgvnauWJjZME\nfgStI+IRNVg=\n-----END CERTIFICATE-----",
		Key:               "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEApQuzGmdrNdflGxXNrKsBAH6VbCtZ5/kvl4OFKwiwEtt6Vinl\ngHhOZVG3nHttYo44lIGw5/Adkcold27665fZF6dmWGTdCmr1cZo9mNib80eLyAEc\n8C3PmwblVoo2+R9PNEac70JYD1MzVe1iI4Gr3h074fdz7mM/ljD6Yn6ED6LbTnzM\nXydGVQcCsSejah+sfATQsxnXVWySle1adV+ze+guzymvvopc8EWQwlHiqRaswMXl\njkxuxDTOKyfopetPJXRLgxCHi9LI90UecTO8nTuubnHhFNauOBB5DYONmNC9GZoP\nOVUEr2RoyQnVKLdqAr6CmOLp3pZn8xvZ6XUYrwIDAQABAoIBAHhP5QnUZeTkMtDh\nvgKmzZ4sqIQnvexKTBUo/MR4GtJESBPTisdx68QUI8LgfsafYkNvnyQUd5m1QEam\nEif3k3uYvhSlwjQ78BwWEdz/2f8oIo9zsEKtQm+CQWAqdRR5bGVxLCmFtWfGgN+c\nojO77SuHKAX7OvmGQ+4aWgu+qkoyg/chIpPXMduAjLMtN3eg60ZqJ5KrKuIF63Bb\nxkPQvzJueB9SfUurmKjUltDMx6G/9RZyS0OIRGyL9Qp8MZ8jE23cXOcDgm0HhkPq\nW4LU++aWAOLYziTjnhjJ+4Iz9R7U8sCmk1wgnK/tapVcJf41R98WuGluyjXpsXgA\nk7vmofECgYEAzuGun9lZ7xGwPifp6vGanWXnW+JiZgCTGuHWgQLIXWYcLfoI3kpH\neLBYINBwvjIQ7P6UxsGSSXd+T82t+8W2LLc2fiKFKE1LVySpH99+cfmIPXxrviOz\nGBX9LTdSCdGkgb54m8aJCpNFnKw5wYgcW1L8CaXXly2Z/KNrGR9R/YUCgYEAzDs4\n19HqlutGLTC30/ziiiIfDaBbX9AzBdUfp9GdT53Mi/7bfxpW/sL4RjG2fGgmN6ua\nfh5npT9AB1ldcEg2qfyOJPt1Ubdi6ek9lx8AB2RMhwdihgX+7bjVMFtjg4b8z5C1\njQbEr1rhFdpaGyNehtAXDgCbDWQBYnBrmM0rCaMCgYBip1Qyfd9ZFcJJoZb2pofo\njvOo6Weq5JNBungjxUPu5gaCFj2sYxd6Af3EiCF7UTypBy3DKgOsbQMa4yYYbcvV\nvviJZcTB1zoaMC1GObl+eFPzniVy4mtBDRtSOJMyg3pDNKUnA6HOHTSQ5cAU/ecn\n1YbCwwbv3JsV0of7zue2UQKBgQCVc0j3dd9rLSQfcaUz9bx5RNrgh9YV2S9dN0aA\n8f1iA6FpWMiazFWY/GfeRga6JyTAXE0juXAzFoPuXNDpl46Y+f2yxmhlsgMqFMpD\nSiYlQppVvWu1k7GnmDg5uMarux5JbiXM24UWpTRNX4nMjidgE+qrDnpoZCQ3Ovkh\nyhGSbQKBgD3VEnPiSUmXBo39kPcnPg93E3JfdAOiOwIB2qwfYzg9kpmuTWws+DFz\nlKpMI27YkmnPqROQ2NTUfdxYmw3EHHMAsvnmHeMNGn3ijSUZVKmPfV436Qc8iVci\ns4wKoCRhBUZ52sHki/ieb+5hycT3JnVXMDtbJxgXFW5a86usXEpO\n-----END RSA PRIVATE KEY-----",
		ClientCertificate: "-----BEGIN CERTIFICATE-----\n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\n-----END CERTIFICATE-----\n",
	},
}

var defaultRootNamespaceID = 1

// apiResponses holds the predefined API responses for certain domains
// that can be used with the GitLab API stub in acceptance tests
var apiResponses = APIResponses{
	"zip-from-disk.gitlab.io": {
		"/": {
			projectID:  123,
			pathOnDisk: "@hashed/zip-from-disk.gitlab.io",
		},
	},
	"zip-not-allowed-path.gitlab.io": {
		"/": {
			pathOnDisk: "../../../../",
		},
	},
	"group.gitlab-example.com": {
		"/": {
			pathOnDisk: "group/group.gitlab-example.com",
		},
		"/CapitalProject": {
			pathOnDisk: "group/CapitalProject",
		},
		"/group.test.io": {
			pathOnDisk: "group/group.test.io",
		},
		"/new-source-test.gitlab.io": {
			pathOnDisk: "group/new-source-test.gitlab.io",
		},
		"/project": {
			pathOnDisk: "group/project",
		},
		"/project2": {
			pathOnDisk: "group/project2",
		},
		"/serving": {
			pathOnDisk: "group/serving",
		},
		"/subgroup/project": {
			pathOnDisk: "group/subgroup/project",
		},
		"/zip.gitlab.io": {
			pathOnDisk: "group/zip.gitlab.io",
		},
	},
	"content-negotiation.gitlab-example.com": {
		"/": {
			pathOnDisk: "group/content-negotiation",
		},
	},
	"group.404.gitlab-example.com": {
		"/": {
			pathOnDisk: "group.404/group.404.gitlab-example.com",
		},
		"/project.404": {
			pathOnDisk: "group.404/project.404",
		},
		"/project.no.404": {
			pathOnDisk: "group/project",
		},
		"/private_project": {
			pathOnDisk:    "group.404/private_project",
			projectID:     1300,
			accessControl: true,
		},
		"/private_unauthorized": {
			pathOnDisk:    "group.404/private_unauthorized",
			projectID:     2000,
			accessControl: true,
		},
	},
	"domain.404.com": {
		"/": {
			projectID:  1000,
			pathOnDisk: "group.404/domain.404",
		},
	},
	"CapitalGroup.gitlab-example.com": {
		"/CapitalProject": {
			pathOnDisk: "/CapitalGroup/CapitalProject",
		},
		"/project": {
			pathOnDisk: "/CapitalGroup/project",
		},
	},
	"group.https-only.gitlab-example.com": {
		"/project1": {
			projectID:  1000,
			httpsOnly:  true,
			pathOnDisk: "group.https-only/project1",
		},
		"/project2": {
			projectID:  1100,
			pathOnDisk: "group.https-only/project2",
		},
		"/project3": {
			pathOnDisk: "group.https-only/project3",
		},
		"/project4": {
			pathOnDisk: "group.https-only/project4",
		},
		"/project5": {
			pathOnDisk: "group.https-only/project5",
		},
		"/project6": {
			pathOnDisk: "group.https-only/project6",
		},
	},
	"withacmechallenge.domain.com": {
		"/": {
			projectID:  1234,
			pathOnDisk: "group.acme/with.acme.challenge",
		},
	},
	"group.redirects.gitlab-example.com": {
		"/": {
			pathOnDisk: "group.redirects/group.redirects.gitlab-example.com",
		},
		"/custom-domain/": {
			pathOnDisk: "group.redirects/custom-domain",
		},
		"/project-redirects/": {
			pathOnDisk: "group.redirects/project-redirects",
		},
	},
	"redirects.custom-domain.com": {
		"/": {
			projectID:  1001,
			pathOnDisk: "group.redirects/custom-domain",
		},
	},
	"test.my-domain.com": {
		"/": {
			projectID:  1002,
			httpsOnly:  true,
			pathOnDisk: "group.https-only/project3",
		},
	},
	"test2.my-domain.com": {
		"/": {
			projectID:  1003,
			httpsOnly:  false,
			pathOnDisk: "group.https-only/project4",
		},
	},
	"no.cert.com": {
		"/": {
			projectID:  1004,
			httpsOnly:  true,
			pathOnDisk: "group.https-only/project5",
		},
	},
	// The namespace path is Group.Auth, while most tests reach it as
	// group.auth: GitLab looks the host up case-insensitively.
	"Group.Auth.gitlab-example.com": {
		"/": {
			projectID:     1005,
			accessControl: true,
			pathOnDisk:    "group.auth/group.auth.gitlab-example.com/",
		},
		"/private.project": {
			projectID:     1006,
			accessControl: true,
			pathOnDisk:    "group.auth/private.project/",
		},
		"/private.project.1": {
			projectID:     2006,
			accessControl: true,
			pathOnDisk:    "group.auth/private.project.1/",
		},
		"/private.project.2": {
			projectID:     3006,
			accessControl: true,
			pathOnDisk:    "group.auth/private.project.2/",
		},
		"/private.project.3": {
			projectID:     4006,
			accessControl: true,
			pathOnDisk:    "group.auth/private.project.3/",
		},
		"/subgroup/private.project": {
			projectID:     1007,
			accessControl: true,
			pathOnDisk:    "group.auth/subgroup/private.project",
		},
		"/subgroup/private.project.1": {
			projectID:     2007,
			accessControl: true,
			pathOnDisk:    "group.auth/subgroup/private.project.1",
		},
		"/subgroup/private.project.2": {
			projectID:     3007,
			accessControl: true,
			pathOnDisk:    "group.auth/subgroup/private.project.2",
		},
	},
	"private.domain.com": {
		"/": {
			projectID:     1007,
			accessControl: true,
			pathOnDisk:    "group.auth/private.project",
		},
	},
	"acmewithredirects.domain.com": {
		"/": {
			projectID:  1008,
			pathOnDisk: "group.acme/with.redirects",
		},
	},
	"mtls.gitlab-example.com": {
		"/": {
			projectID:  1009,
			httpsOnly:  true,
			pathOnDisk: "group.https-only/project6",
		},
	},
	"group.unique-url.gitlab-example.com": {
		"/with-unique-url": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
		"/subgroup1/subgroup2/with-unique-url": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
		"/with-unique-url-with-port": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
		"/with-malformed-unique-url": {
			uniqueHost: "unique-url@gitlab-example.com:",
			pathOnDisk: "group/project",
		},
		"/with-different-protocol": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
		"/without-unique-url": {
			pathOnDisk: "group/project",
		},
	},
	"unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com": {
		"/": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
	},
	"unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com:8080": {
		"/": {
			uniqueHost: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			pathOnDisk: "group/project",
		},
	},
	"custom-root.gitlab-example.com": {
		"/": {
			pathOnDisk:    "group.customroot/customroot",
			rootDirectory: "foo",
		},
	},
	"custom-root-legacy.gitlab-example.com": {
		"/": {
			pathOnDisk: "group/project",
		},
	},
	"custom-root-explicit-public.gitlab-example.com": {
		"/": {
			pathOnDisk:    "group/project",
			rootDirectory: "public",
		},
	},
	"custom-root-no-subdir.gitlab-example.com": {
		"/": {
			pathOnDisk:    "group.customroot/nosubdir",
			rootDirectory: "/",
		},
	},
	"custom-root-wrong-dir.gitlab-example.com": {
		"/": {
			pathOnDisk:    "group/project",
			rootDirectory: "foo",
		},
	},
	"redirect.gitlab-example.com": {
		"/project1": {
			primaryDomain: "https://default.gitlab-example.com",
			pathOnDisk:    "group/project",
		},
		"/subgroup1/subgroup2/project1": {
			primaryDomain: "https://default.gitlab-example.com",
			pathOnDisk:    "group/project",
		},
		"/with-malformed-default-domain-redirect-url": {
			primaryDomain: "://invalid-url",
			pathOnDisk:    "group/project",
		},
	},
	"default.gitlab-example.com": {
		"/": {
			primaryDomain: "https://default.gitlab-example.com",
			pathOnDisk:    "group/project",
		},
	},
	"custom.gitlab-example.com": {
		"/no-redirect/": {
			pathOnDisk: "group/project",
		},
	},
	// NOTE: before adding more domains here, generate the zip archive by running (per project)
	// make zip PROJECT_SUBDIR=group/project2/public
	// make zip PROJECT_SUBDIR=group/customroot/foo
}
