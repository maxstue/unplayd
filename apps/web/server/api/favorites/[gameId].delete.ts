import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { removeFavorite } from "../../bff/handlers";

export default defineEventHandler((event) => removeFavorite(event, useRuntimeConfig(event)));
