import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { searchGames } from "../../bff/handlers";

export default defineEventHandler((event) => searchGames(event, useRuntimeConfig(event)));
