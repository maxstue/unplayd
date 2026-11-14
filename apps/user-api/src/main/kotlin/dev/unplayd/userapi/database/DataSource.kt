package dev.unplayd.userapi.database

import com.zaxxer.hikari.HikariConfig
import com.zaxxer.hikari.HikariDataSource
import org.postgresql.ds.PGSimpleDataSource

fun DatabaseConfig.dataSource(): HikariDataSource =
    HikariDataSource(
        HikariConfig().apply {
            dataSource =
                PGSimpleDataSource().also {
                    it.setURL(this@dataSource.url)
                    it.user = this@dataSource.user
                    it.password = this@dataSource.password
                    it.connectTimeout = 3
                    it.socketTimeout = 5
                    it.cancelSignalTimeout = 2
                    it.applicationName = "user-api"
                }
            poolName = "user-api"
            maximumPoolSize = poolSize
            minimumIdle = 0
            connectionTimeout = connectionTimeoutMs
            validationTimeout = minOf(connectionTimeoutMs, 1000)
            connectionInitSql = "SET statement_timeout = $statementTimeoutMs; SET lock_timeout = $lockTimeoutMs"
        },
    )
