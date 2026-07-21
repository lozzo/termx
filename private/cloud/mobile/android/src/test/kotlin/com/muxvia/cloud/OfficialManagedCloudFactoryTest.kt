package com.muxvia.cloud

import com.muxvia.app.managed.ManagedCloudModuleFactory
import org.junit.Assert.assertTrue
import org.junit.Test

class OfficialManagedCloudFactoryTest {
    @Test
    fun fixedFactoryImplementsPublicContract() {
        val factory: ManagedCloudModuleFactory = OfficialManagedCloudFactory()
        assertTrue(factory.javaClass.name == "com.muxvia.cloud.OfficialManagedCloudFactory")
    }
}
