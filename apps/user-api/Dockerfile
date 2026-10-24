FROM eclipse-temurin:25.0.1_8-jdk AS build
WORKDIR /app
COPY . .
RUN --mount=type=cache,target=/root/.gradle ./gradlew --no-daemon installDist

FROM eclipse-temurin:25.0.1_8-jre
WORKDIR /app
COPY --from=build /app/build/install/user-api .
USER 10001
ENV PORT=8445
EXPOSE 8445
CMD ["bin/user-api"]
