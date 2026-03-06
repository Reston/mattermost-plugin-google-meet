// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import manifest from "manifest";
import type { Store } from "redux";

import type { GlobalState } from "@mattermost/types/store";
import { getCurrentChannelId } from "mattermost-redux/selectors/entities/common";

import type { PluginRegistry } from "types/mattermost-webapp";

import React from "react";

const MenuIcon = () => <i className="icon fa fa-video-camera" />;

const HeaderIcon = () => (
    <i
        className="icon fa fa-video-camera"
        style={{ fontSize: "15px", position: "relative", top: "-1px" }}
    />
);

const createMeeting = async (channelId: string) => {
    const response = await fetch(
        `/plugins/${manifest.id}/api/v1/create?channel_id=${encodeURIComponent(channelId)}`,
        {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
        },
    );

    if (response.status === 401) {
        window.open(`/plugins/${manifest.id}/oauth2/login`, "_blank");
        return;
    }

    if (!response.ok) {
        throw new Error("Failed to create meeting");
    }

    const data = await response.json();
    window.open(data.meet_url, "_blank");
};

export default class Plugin {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars, @typescript-eslint/no-empty-function
    public async initialize(
        registry: PluginRegistry,
        store: Store<GlobalState>,
    ) {
        const handleCreateMeeting = async () => {
            const channelId = getCurrentChannelId(store.getState());
            if (!channelId) {
                return;
            }

            try {
                await createMeeting(channelId);
            } catch (error) {
                // eslint-disable-next-line no-console
                console.error("Error creating meeting:", error);
            }
        };

        const handleChannelMenuMeeting = async (channelId: string) => {
            if (!channelId) {
                return;
            }

            try {
                await createMeeting(channelId);
            } catch (error) {
                // eslint-disable-next-line no-console
                console.error("Error creating meeting:", error);
            }
        };

        registry.registerChannelHeaderButtonAction(
            <HeaderIcon />,
            handleCreateMeeting,
            "Google Meet",
            "Create Google Meet link",
        );

        registry.registerChannelHeaderMenuAction(
            "Create Google Meet",
            handleChannelMenuMeeting,
        );

        registry.registerMainMenuAction(
            "Create Google Meet",
            handleCreateMeeting,
            <MenuIcon />,
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
