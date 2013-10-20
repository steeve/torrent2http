<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<addon id="script.module.torrent2http" name="torrent2http" version="$VERSION" provider-name="steeve">
    <requires>
        <import addon="xbmc.addon" version="12.0.0"/>
        <import addon="xbmc.python" version="2.1.0"/>
    </requires>
    <extension point="xbmc.python.module" library="bin" />
    <extension point="xbmc.addon.metadata">
        <platform>all</platform>
        <source>https://github.com/steeve/torrent2http</source>
        <summary>The torrent part of XBMCtorrent.</summary>
        <description>Converts magnet links into HTTP endpoints.</description>
    </extension>
</addon>
