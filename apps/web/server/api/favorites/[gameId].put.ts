import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { setFavorite } from "../../bff/handlers";

export default defineEventHandler((event) => setFavorite(event, useRuntimeConfig(event)));
