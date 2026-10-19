plugins {
    kotlin("jvm") version "2.4.21"
    kotlin("plugin.serialization") version "2.4.21"
    application
    jacoco
    id("org.jlleitschuh.gradle.ktlint") version "14.0.1"
}

group = "dev.unplayd"
version = "0.1.0"

repositories { mavenCentral() }
kotlin { jvmToolchain(25) }
application { mainClass.set("dev.unplayd.userapi.ApplicationKt") }
dependencyLocking { lockAllConfigurations() }

val ktorVersion = "3.6.0"
dependencies {
    implementation(platform("io.ktor:ktor-bom:$ktorVersion"))
    implementation("io.ktor:ktor-server-netty")
    implementation("io.ktor:ktor-server-content-negotiation")
    implementation("io.ktor:ktor-serialization-kotlinx-json")
    implementation("io.ktor:ktor-server-auth-jwt")
    implementation("io.ktor:ktor-server-status-pages")
    implementation("io.ktor:ktor-server-call-id")
    implementation("ch.qos.logback:logback-classic:1.6.5")
    testImplementation("io.ktor:ktor-server-test-host")
    testImplementation(kotlin("test-junit5"))
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

tasks.test { useJUnitPlatform() }
tasks.jacocoTestReport {
    dependsOn(tasks.test)
    reports { xml.required.set(true) }
}
