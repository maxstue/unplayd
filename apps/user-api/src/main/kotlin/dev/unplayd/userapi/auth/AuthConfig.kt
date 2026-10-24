package dev.unplayd.userapi.auth

import com.auth0.jwk.JwkProvider
import com.auth0.jwk.JwkProviderBuilder
import com.auth0.jwt.JWT
import com.auth0.jwt.algorithms.Algorithm
import com.auth0.jwt.exceptions.JWTVerificationException
import com.auth0.jwt.interfaces.DecodedJWT
import com.auth0.jwt.interfaces.JWTVerifier
import com.auth0.jwt.interfaces.RSAKeyProvider
import java.net.URI
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey
import java.util.concurrent.TimeUnit

class AuthConfig private constructor(
    val issuer: String,
    val authorizedParty: String,
) {
    companion object {
        fun load(env: (String) -> String? = System::getenv): AuthConfig {
            val issuer = origin(env("CLERK_ISSUER"), "CLERK_ISSUER", false)
            val party = origin(env("CLERK_AUTHORIZED_PARTY"), "CLERK_AUTHORIZED_PARTY", true)
            return AuthConfig(issuer, party)
        }

        private fun origin(
            value: String?,
            name: String,
            allowLocal: Boolean,
        ): String {
            val uri = value?.let { runCatching { URI(it) }.getOrNull() }
            require(
                uri != null &&
                    uri.host != null &&
                    uri.userInfo == null &&
                    uri.rawQuery == null &&
                    uri.rawFragment == null &&
                    uri.path.isNullOrEmpty() &&
                    (uri.port == -1 || uri.port in 1..65535),
            ) { "$name must be an origin without credentials, path, query or fragment" }
            require(uri.scheme == "https" || (allowLocal && uri.scheme == "http" && uri.host in setOf("localhost", "127.0.0.1", "[::1]"))) {
                "$name must use HTTPS (HTTP is allowed only for a local authorized party)"
            }
            return uri.toString()
        }
    }

    fun verifier(jwkProvider: JwkProvider? = null): JWTVerifier {
        val keys =
            jwkProvider
                ?: JwkProviderBuilder(
                    URI("$issuer/.well-known/jwks.json").toURL(),
                ).cached(10, 1, TimeUnit.HOURS).rateLimited(10, 1, TimeUnit.MINUTES).build()
        val algorithm =
            Algorithm.RSA256(
                object : RSAKeyProvider {
                    override fun getPublicKeyById(keyId: String?): RSAPublicKey {
                        if (keyId.isNullOrBlank() || keyId.length > 256) throw JWTVerificationException("JWT key ID is required")
                        return try {
                            keys.get(keyId).publicKey as RSAPublicKey
                        } catch (_: Exception) {
                            throw JWTVerificationException("Signing key unavailable")
                        }
                    }

                    override fun getPrivateKey(): RSAPrivateKey? = null

                    override fun getPrivateKeyId(): String? = null
                },
            )
        val verifier =
            JWT
                .require(algorithm)
                .withIssuer(issuer)
                .withClaim("azp", authorizedParty)
                .withClaim("sub") { claim, _ -> claim.asString()?.matches(Regex("user_[a-zA-Z0-9]+")) == true }
                .withClaim("sid") { claim, _ -> claim.asString()?.matches(Regex("sess_[a-zA-Z0-9]+")) == true }
                .withClaimPresence("exp")
                .withClaimPresence("iat")
                .withClaimPresence("nbf")
                .acceptLeeway(5)
                .build()
        return object : JWTVerifier {
            override fun verify(token: String): DecodedJWT = verify(JWT.decode(token))

            override fun verify(token: DecodedJWT): DecodedJWT =
                verifier.verify(token).also {
                    val status = it.getClaim("sts")
                    if (!status.isMissing && status.asString() != "active") throw JWTVerificationException("Inactive session")
                }
        }
    }
}
