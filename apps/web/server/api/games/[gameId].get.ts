import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { getGame } from "../../bff/handlers";

export default defineEventHandler((event) => getGame(event, useRuntimeConfig(event)));
