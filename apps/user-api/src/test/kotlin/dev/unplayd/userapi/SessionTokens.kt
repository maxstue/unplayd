package dev.unplayd.userapi

import com.auth0.jwk.Jwk
import com.auth0.jwk.JwkProvider
import com.auth0.jwt.JWT
import com.auth0.jwt.algorithms.Algorithm
import java.math.BigInteger
import java.security.KeyPairGenerator
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey
import java.time.Instant
import java.util.Base64
import java.util.UUID

internal class SessionTokens {
    private val keys = KeyPairGenerator.getInstance("RSA").apply { initialize(2048) }.generateKeyPair()
    private val publicKey = keys.public as RSAPublicKey
    private val keyId = UUID.randomUUID().toString()
    private val algorithm = Algorithm.RSA256(publicKey, keys.private as RSAPrivateKey)
    private val jwk =
        Jwk.fromValues(
            mapOf(
                "kid" to keyId,
                "kty" to "RSA",
                "alg" to "RS256",
                "use" to "sig",
                "n" to publicKey.modulus.base64Url(),
                "e" to publicKey.publicExponent.base64Url(),
            ),
        )

    val provider =
        JwkProvider { requestedId ->
            require(requestedId == keyId) { "Unknown test key" }
            jwk
        }

    fun token(
        subject: String,
        claims: Map<String, Any?> = emptyMap(),
        signingAlgorithm: Algorithm = algorithm,
        signingKeyId: String? = keyId,
    ): String {
        val now = Instant.now().epochSecond
        val payload =
            mapOf(
                "iss" to "https://clerk.test",
                "sub" to subject,
                "azp" to "http://localhost:3000",
                "sid" to "sess_${UUID.randomUUID().toString().replace("-", "")}",
                "iat" to now - 60,
                "nbf" to now - 60,
                "exp" to now + 3600,
            ) + claims
        return JWT
            .create()
            .withKeyId(signingKeyId)
            .withPayload(payload.filterValues { it != null })
            .sign(signingAlgorithm)
    }

    fun tamperedToken(subject: String): String {
        val parts = token(subject).split(".")
        val signature = Base64.getUrlDecoder().decode(parts[2])
        signature[0] = (signature[0].toInt() xor 1).toByte()
        return parts.take(2).joinToString(".") + "." + Base64.getUrlEncoder().withoutPadding().encodeToString(signature)
    }

    private fun BigInteger.base64Url(): String =
        Base64.getUrlEncoder().withoutPadding().encodeToString(toByteArray().dropWhile { it == 0.toByte() }.toByteArray())
}
