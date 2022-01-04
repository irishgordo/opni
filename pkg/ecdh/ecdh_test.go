package ecdh_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kralicky/opni-gateway/pkg/ecdh"
	"github.com/kralicky/opni-gateway/pkg/keyring"
)

var _ = Describe("ECDH", func() {
	It("should generate a key pair", func() {
		ekp, err := ecdh.NewEphemeralKeyPair()
		Expect(err).NotTo(HaveOccurred())
		Expect(ekp.PrivateKey).NotTo(BeNil())
		Expect(ekp.PublicKey).NotTo(BeNil())
	})

	It("should compute equal shared secrets", func() {
		ekpA, err := ecdh.NewEphemeralKeyPair()
		Expect(err).NotTo(HaveOccurred())
		Expect(ekpA.PrivateKey).NotTo(BeNil())
		Expect(ekpA.PublicKey).NotTo(BeNil())
		ekpB, err := ecdh.NewEphemeralKeyPair()
		Expect(err).NotTo(HaveOccurred())
		Expect(ekpB.PrivateKey).NotTo(BeNil())
		Expect(ekpB.PublicKey).NotTo(BeNil())

		Expect(ekpA.PrivateKey).NotTo(Equal(ekpB.PrivateKey))
		Expect(ekpA.PublicKey).NotTo(Equal(ekpB.PublicKey))

		secretA, err := ecdh.DeriveSharedSecret(ekpA, ecdh.PeerPublicKey{
			PublicKey: ekpB.PublicKey,
			PeerType:  ecdh.PeerTypeServer,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(secretA).NotTo(BeNil())

		secretB, err := ecdh.DeriveSharedSecret(ekpB, ecdh.PeerPublicKey{
			PublicKey: ekpA.PublicKey,
			PeerType:  ecdh.PeerTypeClient,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(secretB).NotTo(BeNil())

		Expect(secretA).To(Equal(secretB))
	})
	It("should generate equal client keys", func() {
		ekpA, err := ecdh.NewEphemeralKeyPair()
		Expect(err).NotTo(HaveOccurred())
		ekpB, err := ecdh.NewEphemeralKeyPair()
		Expect(err).NotTo(HaveOccurred())

		secretA, err := ecdh.DeriveSharedSecret(ekpA, ecdh.PeerPublicKey{
			PublicKey: ekpB.PublicKey,
			PeerType:  ecdh.PeerTypeServer,
		})
		Expect(err).NotTo(HaveOccurred())

		secretB, err := ecdh.DeriveSharedSecret(ekpB, ecdh.PeerPublicKey{
			PublicKey: ekpA.PublicKey,
			PeerType:  ecdh.PeerTypeClient,
		})
		Expect(err).NotTo(HaveOccurred())

		kr1 := keyring.NewSharedKeys(secretA)
		kr2 := keyring.NewSharedKeys(secretB)

		Expect(kr1.ClientKey).To(Equal(kr2.ClientKey))
		Expect(kr1.ServerKey).To(Equal(kr2.ServerKey))
	})
})
