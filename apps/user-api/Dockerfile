FROM eclipse-temurin:25.0.5.1_2-jdk AS build
WORKDIR /app
# Dependencies get their own layer instead of a BuildKit cache mount, which Railway only
# accepts with a service-specific id.
COPY gradlew settings.gradle.kts build.gradle.kts gradle.lockfile ./
COPY gradle gradle
RUN ./gradlew --no-daemon dependencies --configuration runtimeClasspath > /dev/null
COPY src src
RUN ./gradlew --no-daemon installDist

FROM eclipse-temurin:25.0.5.1_2-jre
WORKDIR /app
COPY --from=build /app/build/install/user-api .
USER 10001
ENV PORT=8445
EXPOSE 8445
CMD ["bin/user-api"]
