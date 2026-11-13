package dev.unplayd.userapi.library

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.time.Instant
import java.util.Base64
import java.util.UUID

internal val ownerships = setOf("owned", "wishlist", "subscription")
internal val statuses = setOf("backlog", "playing", "paused", "completed", "dropped")

class LibraryProblem(
    val status: Int,
    override val message: String,
) : RuntimeException(message)

internal fun uuid(value: String?): UUID =
    value
        ?.takeIf { it.matches(Regex("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}")) }
        ?.let(UUID::fromString) ?: throw LibraryProblem(400, "Invalid UUID")

@Serializable
data class GameSnapshot(
    val title: String,
    val coverUrl: String?,
)

@Serializable
data class LibraryEntry(
    val id: String,
    val gameId: String,
    val platformId: String?,
    val ownership: String,
    val status: String,
    val favorite: Boolean,
    val rating: Int?,
    val note: String?,
    val snapshot: GameSnapshot,
    val createdAt: String,
    val updatedAt: String,
)

@Serializable
data class AddLibraryEntry(
    val gameId: String,
    val ownership: String,
    val platformId: String? = null,
) {
    fun validate() {
        uuid(gameId)
        platformId?.let(::uuid)
        if (ownership !in ownerships) throw LibraryProblem(400, "Invalid ownership")
    }
}

@Serializable
data class SetStatus(
    val status: String,
)

@Serializable
data class LibraryPage(
    val items: List<LibraryEntry>,
    val nextCursor: String?,
)

@Serializable
internal data class LibraryCursor(
    val createdAt: String,
    val id: String,
    val status: String?,
    val ownership: String?,
    val platformId: String?,
) {
    fun encode(): String = Base64.getUrlEncoder().withoutPadding().encodeToString(Json.encodeToString(this).toByteArray())
}

class LibraryFilter(
    val status: String? = null,
    val ownership: String? = null,
    platformId: String? = null,
    val limit: Int = 20,
    cursor: String? = null,
) {
    val platformId = platformId?.let { uuid(it).toString() }
    internal val after: LibraryCursor?

    init {
        if (status != null && status !in statuses || ownership != null && ownership !in ownerships || limit !in 1..50) {
            throw LibraryProblem(400, "Invalid library filter")
        }
        after =
            cursor?.let {
                try {
                    require(it.length in 1..512)
                    Json.decodeFromString<LibraryCursor>(Base64.getUrlDecoder().decode(it).decodeToString()).also { value ->
                        Instant.parse(value.createdAt)
                        uuid(value.id)
                        require(value.status == status && value.ownership == ownership && value.platformId == this.platformId)
                    }
                } catch (_: Exception) {
                    throw LibraryProblem(400, "Invalid library cursor")
                }
            }
    }
}

@Serializable
data class LibraryOverview(
    val total: Int,
    val statuses: Map<String, Int>,
)

@Serializable
data class LibraryPlatform(
    val platformId: String,
    val gameId: String,
)

@Serializable
data class LibraryPlatforms(
    val items: List<LibraryPlatform>,
)
